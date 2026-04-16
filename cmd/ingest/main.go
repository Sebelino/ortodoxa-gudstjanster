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
	if openaiAPIKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

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
	registry.Register(scraper.NewSrpskaScraper(visionClient))
	registry.Register(scraper.NewGCalendarScraper())
	registry.Register(scraper.NewGCalendarManualScraper())
	registry.Register(scraper.NewRomanianScraper())
	registry.Register(scraper.NewSommarlagerScraper(gcsStore, visionClient))
	registry.Register(scraper.NewManualScraper(manualEventsReader))
	if uploadReader != nil {
		registry.Register(scraper.NewUploadsScraper(gcsStore, visionClient, uploadReader, gcsUploadBucket))
	}

	// Generate batch ID for this ingestion run
	batchID := time.Now().UTC().Format("20060102-150405")
	log.Printf("Starting ingestion with batch ID: %s", batchID)

	today := time.Now().Format("2006-01-02")

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
			// Compare future service counts to detect regressions
			newCount := 0
			for _, svc := range services {
				if svc.Date >= today {
					newCount++
				}
			}
			existingCount, err := fsClient.CountFutureServicesForScraper(ctx, scraperName)
			if err != nil {
				log.Printf("WARNING: Failed to count existing services for %s: %v", scraperName, err)
				// Proceed with replacement if we can't count
			} else if newCount < existingCount {
				log.Printf("WARNING: Scraper %s returned fewer future services (%d) than currently stored (%d). Skipping replacement.",
					scraperName, newCount, existingCount)

				// Save rejected data to GCS for diagnostics
				gcsPath := saveDiagnostics(gcsStore, scraperName, services)

				// Send alert email if SMTP is configured
				if smtpConfig != nil {
					subject := fmt.Sprintf("Ingestion alert: %s service count decreased", scraperName)
					body := fmt.Sprintf(
						"Scraper: %s\r\nExisting future count: %d\r\nNew future count: %d\r\nAction: Skipped replacement (existing data preserved)\r\nRejected data: gs://%s/%s",
						scraperName, existingCount, newCount, gcsBucket, gcsPath,
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

	// Time parsing: deterministic parsing with correct DST handling
	timeMap := parseTimes(accepted)

	// Event language parsing: detect explicit language mentions in service names
	eventLangMap := parseEventLanguages(accepted)

	// Pass 2: Annotate services with titles, times, and languages, then write to Firestore
	totalServices := 0
	for _, result := range accepted {
		for i := range result.services {
			// Only apply generated title if the scraper didn't set one explicitly
			if result.services[i].Title == "" {
				if title, ok := titleMap[result.services[i].ServiceName]; ok {
					result.services[i].Title = title
				}
			}
			if result.services[i].Time != nil {
				key := result.services[i].Date + "|" + *result.services[i].Time
				if pt, ok := timeMap[key]; ok {
					result.services[i].StartTime = &pt.Start
					result.services[i].EndTime = pt.End
				}
			}
			// Copy Language → ParishLanguage if not already set by the scraper
			if result.services[i].Language != nil && result.services[i].ParishLanguage == nil {
				result.services[i].ParishLanguage = result.services[i].Language
			}
			// Set EventLanguage from parsed results, but only if detected and not already set
			if result.services[i].EventLanguage == nil {
				mapKey := eventLangMapKey(result.services[i].ServiceName, result.services[i].Occasion, result.services[i].Notes)
				if lang, ok := eventLangMap[mapKey]; ok && lang != nil {
					result.services[i].EventLanguage = lang
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

	// Pass 3: Apply persistent field overrides from overrides.json in the manual events bucket
	applyOverrides(ctx, manualEventsReader, accepted, fsClient)

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

// parseTimes deterministically parses (date, time) pairs into Stockholm-timezone
// timestamps. Handles formats like "18:00", "18:00 - 20:00", "14:30 - ca 16:00".
// DST is handled correctly via time.LoadLocation.
func parseTimes(accepted []acceptedResult) map[string]vision.ParsedTime {
	stockholm, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		panic(fmt.Sprintf("failed to load Europe/Stockholm timezone: %v", err))
	}

	type dateTime struct {
		date, timeStr string
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
				pairs = append(pairs, dateTime{date: svc.Date, timeStr: *svc.Time})
			}
		}
	}

	timeMap := make(map[string]vision.ParsedTime)
	for _, pair := range pairs {
		pt, err := parseTimeString(pair.date, pair.timeStr, stockholm)
		if err != nil {
			log.Printf("WARNING: skipping unparseable time %q for %s: %v", pair.timeStr, pair.date, err)
			continue
		}
		key := pair.date + "|" + pair.timeStr
		timeMap[key] = pt
	}

	log.Printf("Parsed %d time entries", len(timeMap))
	return timeMap
}

// parseTimeString parses a time string like "18:00" or "18:00 - ca 20:00"
// combined with a date string into a ParsedTime in the given timezone.
func parseTimeString(dateStr, timeStr string, loc *time.Location) (vision.ParsedTime, error) {
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return vision.ParsedTime{}, fmt.Errorf("invalid date %q: %w", dateStr, err)
	}

	makeTime := func(hhmm string) (time.Time, error) {
		// Strip common prefixes
		s := strings.TrimSpace(hhmm)
		s = strings.TrimPrefix(s, "ca ")
		s = strings.TrimPrefix(s, "ca. ")
		s = strings.TrimPrefix(s, "kl ")
		s = strings.TrimPrefix(s, "kl. ")
		s = strings.TrimSpace(s)

		var h, m int
		if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
			return time.Time{}, fmt.Errorf("invalid time %q: %w", hhmm, err)
		}
		return time.Date(date.Year(), date.Month(), date.Day(), h, m, 0, 0, loc), nil
	}

	// Check for range: "HH:MM - HH:MM" or "HH:MM - ca HH:MM"
	if parts := strings.SplitN(timeStr, " - ", 2); len(parts) == 2 {
		start, err := makeTime(parts[0])
		if err != nil {
			return vision.ParsedTime{}, err
		}
		end, err := makeTime(parts[1])
		if err != nil {
			return vision.ParsedTime{}, err
		}
		// Handle midnight crossing
		if end.Before(start) {
			end = end.AddDate(0, 0, 1)
		}
		return vision.ParsedTime{Start: start, End: &end}, nil
	}

	start, err := makeTime(timeStr)
	if err != nil {
		return vision.ParsedTime{}, err
	}
	return vision.ParsedTime{Start: start}, nil
}

// eventLangMapKey returns a deduplication key for an event's relevant fields.
// Uses a hash to avoid collisions from field values containing the separator.
func eventLangMapKey(serviceName string, occasion, notes *string) string {
	occ := ""
	if occasion != nil {
		occ = *occasion
	}
	n := ""
	if notes != nil {
		n = *notes
	}
	data := fmt.Sprintf("%d:%s\n%d:%s\n%d:%s", len(serviceName), serviceName, len(occ), occ, len(n), n)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:16])
}


// languagePatterns maps Swedish language phrases to their canonical language name.
var languagePatterns = []struct {
	pattern  string
	language string
}{
	{"på svenska", "Svenska"},
	{"på engelska", "Engelska"},
	{"på finska", "Finska"},
	{"på grekiska", "Grekiska"},
	{"på arabiska", "Arabiska"},
	{"på kyrkoslaviska", "Kyrkoslaviska"},
	{"på rumänska", "Rumänska"},
	{"på serbiska", "Serbiska"},
	{"på georgiska", "Georgiska"},
	{"på bulgariska", "Bulgariska"},
}

// detectEventLanguage checks if any field explicitly mentions a language.
func detectEventLanguage(serviceName string, occasion, notes *string) *string {
	fields := []string{strings.ToLower(serviceName)}
	if occasion != nil {
		fields = append(fields, strings.ToLower(*occasion))
	}
	if notes != nil {
		fields = append(fields, strings.ToLower(*notes))
	}
	for _, lp := range languagePatterns {
		for _, f := range fields {
			if strings.Contains(f, lp.pattern) {
				lang := lp.language
				return &lang
			}
		}
	}
	return nil
}

// parseEventLanguages detects explicit language mentions in event fields
// and returns a map keyed by eventLangMapKey → *string (nil = no explicit language).
func parseEventLanguages(accepted []acceptedResult) map[string]*string {
	seen := make(map[string]struct{})
	langMap := make(map[string]*string)

	for _, result := range accepted {
		for _, svc := range result.services {
			mapKey := eventLangMapKey(svc.ServiceName, svc.Occasion, svc.Notes)
			if _, ok := seen[mapKey]; ok {
				continue
			}
			seen[mapKey] = struct{}{}
			langMap[mapKey] = detectEventLanguage(svc.ServiceName, svc.Occasion, svc.Notes)
		}
	}

	detected := 0
	for _, v := range langMap {
		if v != nil {
			detected++
		}
	}
	log.Printf("Event languages: %d detected out of %d unique events", detected, len(langMap))
	return langMap
}

// serviceOverride defines a match criterion and the fields to patch.
type serviceOverride struct {
	Match  overrideMatch          `json:"match"`
	Fields map[string]interface{} `json:"fields"`
}

// overrideMatch identifies a service by parish, date, and time prefix.
// Time is matched as a prefix so "23:00" matches "23:00 - 02:00".
type overrideMatch struct {
	Parish string `json:"parish"`
	Date   string `json:"date"`
	Time   string `json:"time"`
}

// applyOverrides reads overrides.json from the manual events bucket and patches
// matching Firestore documents with the specified field values.
func applyOverrides(ctx context.Context, reader *store.BucketReader, accepted []acceptedResult, fsClient *firestore.Client) {
	if reader == nil {
		return
	}

	data, err := reader.ReadObject(ctx, "overrides.json")
	if err != nil {
		// File not present is fine — no overrides configured
		return
	}

	var overrides []serviceOverride
	if err := json.Unmarshal(data, &overrides); err != nil {
		log.Printf("WARNING: Failed to parse overrides.json: %v", err)
		return
	}

	applied := 0
	for _, ov := range overrides {
		for _, result := range accepted {
			for _, svc := range result.services {
				if svc.Parish != ov.Match.Parish || svc.Date != ov.Match.Date {
					continue
				}
				if svc.Time == nil || !strings.HasPrefix(*svc.Time, ov.Match.Time) {
					continue
				}
				if err := fsClient.PatchService(ctx, svc, ov.Fields); err != nil {
					log.Printf("WARNING: Override failed for %s %s %s: %v", svc.Parish, svc.Date, *svc.Time, err)
				} else {
					log.Printf("Override applied: %s %s %s → %v", svc.Parish, svc.Date, *svc.Time, ov.Fields)
					applied++
				}
			}
		}
	}

	log.Printf("Overrides: %d applied out of %d defined", applied, len(overrides))
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
