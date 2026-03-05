package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"ortodoxa-gudstjanster/internal/email"
	"ortodoxa-gudstjanster/internal/firestore"
	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/scraper"
	"ortodoxa-gudstjanster/internal/store"
	"ortodoxa-gudstjanster/internal/vision"
)

func main() {
	ctx := context.Background()

	// Required environment variables
	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		log.Fatal("GCP_PROJECT_ID environment variable is required")
	}

	firestoreCollection := os.Getenv("FIRESTORE_COLLECTION")
	if firestoreCollection == "" {
		firestoreCollection = "services"
	}

	gcsBucket := os.Getenv("GCS_BUCKET")
	if gcsBucket == "" {
		log.Fatal("GCS_BUCKET environment variable is required")
	}

	openaiAPIKey := os.Getenv("OPENAI_API_KEY")

	// Initialize GCS store
	gcsStore, err := store.NewGCS(ctx, gcsBucket)
	if err != nil {
		log.Fatalf("Failed to initialize GCS store: %v", err)
	}
	log.Printf("Store: GCS bucket %s", gcsBucket)

	// Initialize vision client
	visionClient := vision.NewClient(openaiAPIKey)

	// Initialize Firestore client
	fsClient, err := firestore.New(ctx, projectID, firestoreCollection)
	if err != nil {
		log.Fatalf("Failed to initialize Firestore client: %v", err)
	}
	defer fsClient.Close()
	log.Printf("Firestore: project %s, collection %s", projectID, firestoreCollection)

	// Initialize upload bucket reader (optional)
	gcsUploadBucket := os.Getenv("GCS_UPLOAD_BUCKET")
	var uploadReader *store.BucketReader
	if gcsUploadBucket != "" {
		var err2 error
		uploadReader, err2 = store.NewBucketReader(ctx, gcsUploadBucket)
		if err2 != nil {
			log.Fatalf("Failed to initialize upload bucket reader: %v", err2)
		}
		defer uploadReader.Close()
		log.Printf("Upload bucket: %s", gcsUploadBucket)
	}

	// Initialize manual events bucket reader (optional)
	gcsManualEventsBucket := os.Getenv("GCS_MANUAL_EVENTS_BUCKET")
	var manualEventsReader *store.BucketReader
	if gcsManualEventsBucket != "" {
		var err2 error
		manualEventsReader, err2 = store.NewBucketReader(ctx, gcsManualEventsBucket)
		if err2 != nil {
			log.Fatalf("Failed to initialize manual events bucket reader: %v", err2)
		}
		defer manualEventsReader.Close()
		log.Printf("Manual events bucket: %s", gcsManualEventsBucket)
	}

	// Initialize SMTP for alerting (optional)
	var smtpConfig *email.SMTPConfig
	if smtpHost := strings.TrimSpace(os.Getenv("SMTP_HOST")); smtpHost != "" {
		smtpConfig = &email.SMTPConfig{
			Host:     smtpHost,
			Port:     strings.TrimSpace(os.Getenv("SMTP_PORT")),
			User:     strings.TrimSpace(os.Getenv("SMTP_USER")),
			Password: strings.TrimSpace(os.Getenv("SMTP_PASS")),
			To:       strings.TrimSpace(os.Getenv("SMTP_TO")),
		}
		log.Printf("SMTP configured for alerting: %s -> %s", smtpConfig.User, smtpConfig.To)
	} else {
		log.Printf("SMTP not configured (alerts disabled)")
	}

	// Initialize scraper registry and register all scrapers
	registry := scraper.NewRegistry()
	registry.Register(scraper.NewFinskaScraper(""))
	gomosScraper := scraper.NewGomosScraper(gcsStore, visionClient)
	if uploadReader != nil {
		gomosScraper.SetUploadSource(uploadReader, "gomos/")
	}
	registry.Register(gomosScraper)
	registry.Register(scraper.NewHeligaAnnaScraper())
	registry.Register(scraper.NewRyskaScraper(gcsStore, visionClient))
	registry.Register(scraper.NewSrpskaScraper())
	registry.Register(scraper.NewManualScraper(manualEventsReader))

	// Generate batch ID for this ingestion run
	batchID := time.Now().UTC().Format("20060102-150405")
	log.Printf("Starting ingestion with batch ID: %s", batchID)

	// Pass 1: Run scrapers and collect accepted results
	scrapers := registry.Scrapers()
	var accepted []acceptedResult
	failedScrapers := 0

	for _, s := range scrapers {
		scraperName := s.Name()
		log.Printf("Running scraper: %s", scraperName)

		services, err := s.Fetch(ctx)
		if err != nil {
			log.Printf("ERROR: Scraper %s failed: %v", scraperName, err)
			failedScrapers++
			continue
		}

		log.Printf("Scraper %s fetched %d services", scraperName, len(services))

		if len(services) > 0 {
			// Check if the new count is less than the existing count
			existingCount, err := fsClient.CountServicesForScraper(ctx, scraperName)
			if err != nil {
				log.Printf("WARNING: Failed to count existing services for %s: %v", scraperName, err)
				// Proceed with replacement if we can't count
			} else if len(services) < existingCount {
				log.Printf("WARNING: Scraper %s returned fewer services (%d) than currently stored (%d). Skipping replacement.",
					scraperName, len(services), existingCount)

				// Save rejected data to GCS for diagnostics
				gcsPath := saveDiagnostics(gcsStore, scraperName, services)

				// Send alert email if SMTP is configured
				if smtpConfig != nil {
					subject := fmt.Sprintf("Ingestion alert: %s service count decreased", scraperName)
					body := fmt.Sprintf(
						"Scraper: %s\r\nExisting count: %d\r\nNew count: %d\r\nAction: Skipped replacement (existing data preserved)\r\nRejected data: gs://%s/%s",
						scraperName, existingCount, len(services), gcsBucket, gcsPath,
					)
					if err := smtpConfig.Send(subject, body); err != nil {
						log.Printf("ERROR: Failed to send alert email for %s: %v", scraperName, err)
					} else {
						log.Printf("Alert email sent for %s", scraperName)
					}
				}

				continue
			}

			accepted = append(accepted, acceptedResult{scraperName: scraperName, services: services})
		}
	}

	// Title generation: collect unique service names, look up cache, call AI for uncached
	titleMap := generateTitles(ctx, accepted, visionClient, gcsStore)

	// Time parsing: collect unique (date, time) pairs, look up cache, call AI for uncached
	timeMap := parseTimes(ctx, accepted, visionClient, gcsStore)

	// Pass 2: Annotate services with titles and times, then write to Firestore
	totalServices := 0
	for _, result := range accepted {
		for i := range result.services {
			if title, ok := titleMap[result.services[i].ServiceName]; ok {
				result.services[i].Title = title
			}
			if result.services[i].Time != nil {
				key := result.services[i].Date + "|" + *result.services[i].Time
				if pt, ok := timeMap[key]; ok {
					result.services[i].StartTime = &pt.Start
					result.services[i].EndTime = pt.End
				}
			}
		}

		if err := fsClient.ReplaceServicesForScraper(ctx, result.scraperName, result.services, batchID); err != nil {
			log.Printf("ERROR: Failed to store services for %s: %v", result.scraperName, err)
			failedScrapers++
			continue
		}
		log.Printf("Stored %d services for %s", len(result.services), result.scraperName)
		totalServices += len(result.services)
	}

	log.Printf("Ingestion complete. Total services: %d, Failed scrapers: %d/%d",
		totalServices, failedScrapers, len(scrapers))

	if failedScrapers > 0 {
		os.Exit(1)
	}
	fmt.Println("Ingestion completed successfully")
}

type acceptedResult struct {
	scraperName string
	services    []model.ChurchService
}

// titleCacheKey returns the GCS cache key for a service name's title.
func titleCacheKey(serviceName string) string {
	hash := sha256.Sum256([]byte(serviceName))
	return "titles/v1/" + hex.EncodeToString(hash[:])
}

// generateTitles collects unique service names from accepted results, checks the
// GCS cache for existing titles, calls the AI for uncached names, and returns
// a complete service_name → title map. Failures are non-fatal.
func generateTitles(ctx context.Context, accepted []acceptedResult, visionClient *vision.Client, gcsStore *store.GCSStore) map[string]string {
	// Collect unique service names
	nameSet := make(map[string]struct{})
	for _, result := range accepted {
		for _, svc := range result.services {
			nameSet[svc.ServiceName] = struct{}{}
		}
	}

	titleMap := make(map[string]string)
	var uncached []string

	// Check cache for each name
	for name := range nameSet {
		key := titleCacheKey(name)
		var title string
		if gcsStore.GetJSON(key, &title) {
			titleMap[name] = title
		} else {
			uncached = append(uncached, name)
		}
	}

	log.Printf("Titles: %d cached, %d uncached", len(titleMap), len(uncached))

	if len(uncached) == 0 {
		return titleMap
	}

	// Call AI for uncached names
	generated, err := visionClient.GenerateTitles(ctx, uncached)
	if err != nil {
		log.Printf("WARNING: Title generation failed (proceeding without titles): %v", err)
		return titleMap
	}

	// Cache and merge results
	for name, title := range generated {
		titleMap[name] = title
		key := titleCacheKey(name)
		if err := gcsStore.SetJSON(key, title); err != nil {
			log.Printf("WARNING: Failed to cache title for %q: %v", name, err)
		}
	}

	log.Printf("Generated %d titles", len(generated))
	return titleMap
}

// timeCacheKey returns the GCS cache key for a (date, time) pair.
func timeCacheKey(date, timeStr string) string {
	hash := sha256.Sum256([]byte(date + "|" + timeStr))
	return "times/v1/" + hex.EncodeToString(hash[:])
}

// cachedTime is the JSON structure stored in GCS for cached time parsing results.
type cachedTime struct {
	Start string  `json:"start"`
	End   *string `json:"end"`
}

// parseTimes collects unique (date, time) pairs from accepted results, checks the
// GCS cache, calls the AI for uncached pairs, and returns a complete map. Non-fatal.
func parseTimes(ctx context.Context, accepted []acceptedResult, visionClient *vision.Client, gcsStore *store.GCSStore) map[string]vision.ParsedTime {
	// Collect unique (date, time) pairs
	type dateTime struct {
		date, time string
	}
	seen := make(map[string]struct{})
	var pairs []dateTime
	for _, result := range accepted {
		for _, svc := range result.services {
			if svc.Time == nil {
				continue
			}
			key := svc.Date + "|" + *svc.Time
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				pairs = append(pairs, dateTime{date: svc.Date, time: *svc.Time})
			}
		}
	}

	timeMap := make(map[string]vision.ParsedTime)
	var uncached []vision.TimeEntry

	// Check cache for each pair
	for _, pair := range pairs {
		key := timeCacheKey(pair.date, pair.time)
		var ct cachedTime
		if gcsStore.GetJSON(key, &ct) {
			start, err := time.Parse(time.RFC3339, ct.Start)
			if err != nil {
				uncached = append(uncached, vision.TimeEntry{Date: pair.date, Time: pair.time})
				continue
			}
			pt := vision.ParsedTime{Start: start}
			if ct.End != nil {
				end, err := time.Parse(time.RFC3339, *ct.End)
				if err == nil {
					pt.End = &end
				}
			}
			mapKey := pair.date + "|" + pair.time
			timeMap[mapKey] = pt
		} else {
			uncached = append(uncached, vision.TimeEntry{Date: pair.date, Time: pair.time})
		}
	}

	log.Printf("Times: %d cached, %d uncached", len(timeMap), len(uncached))

	if len(uncached) == 0 {
		return timeMap
	}

	// Call AI for uncached pairs
	parsed, err := visionClient.ParseTimes(ctx, uncached)
	if err != nil {
		log.Printf("WARNING: Time parsing failed (proceeding without timestamps): %v", err)
		return timeMap
	}

	// Cache and merge results
	for _, entry := range uncached {
		mapKey := entry.Date + "|" + entry.Time
		pt, ok := parsed[mapKey]
		if !ok {
			continue
		}
		timeMap[mapKey] = pt

		// Cache result
		ct := cachedTime{Start: pt.Start.Format(time.RFC3339)}
		if pt.End != nil {
			endStr := pt.End.Format(time.RFC3339)
			ct.End = &endStr
		}
		cacheKey := timeCacheKey(entry.Date, entry.Time)
		if err := gcsStore.SetJSON(cacheKey, ct); err != nil {
			log.Printf("WARNING: Failed to cache time for %s %s: %v", entry.Date, entry.Time, err)
		}
	}

	log.Printf("Parsed %d time entries", len(parsed))
	return timeMap
}

// saveDiagnostics serializes rejected services to GCS and returns the object path.
func saveDiagnostics(gcsStore *store.GCSStore, scraperName string, services []model.ChurchService) string {
	timestamp := time.Now().UTC().Format("20060102-150405")
	// Sanitize scraper name for use in path
	safeName := strings.ReplaceAll(strings.ToLower(scraperName), " ", "-")
	path := fmt.Sprintf("diagnostics/%s/%s.json", safeName, timestamp)

	data, err := json.MarshalIndent(services, "", "  ")
	if err != nil {
		log.Printf("WARNING: Failed to marshal diagnostics for %s: %v", scraperName, err)
		return path
	}

	if err := gcsStore.SetRaw(path, data); err != nil {
		log.Printf("WARNING: Failed to save diagnostics for %s to %s: %v", scraperName, path, err)
	} else {
		log.Printf("Saved rejected data for %s to %s", scraperName, path)
	}

	return path
}
