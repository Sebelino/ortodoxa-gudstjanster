package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"ortodoxa-gudstjanster/internal/email"
	"ortodoxa-gudstjanster/internal/firestore"
	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/scraper"
	"ortodoxa-gudstjanster/internal/store"
	"ortodoxa-gudstjanster/internal/umap"
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

	// Sync parishes from uMap to Firestore and build slug/name indexes for resolution.
	// Retry up to 3 times with increasing delays to handle transient API errors.
	var umapParishes []umap.Parish
	for attempt := 1; attempt <= 3; attempt++ {
		umapParishes, err = umap.FetchParishes()
		if err == nil {
			break
		}
		if attempt < 3 {
			log.Printf("WARNING: uMap fetch attempt %d/3 failed: %v, retrying in %ds...", attempt, err, attempt*3)
			time.Sleep(time.Duration(attempt*3) * time.Second)
		}
	}
	slugToParish := make(map[string]umap.Parish)
	parishNameToSlug := make(map[string]string)
	for _, p := range umapParishes {
		slugToParish[p.Slug] = p
		parishNameToSlug[p.Name] = p.Slug
	}
	if err != nil {
		log.Printf("WARNING: failed to fetch parishes from uMap after 3 attempts: %v", err)
	} else if len(umapParishes) > 0 {
		if err := fsClient.SaveParishes(ctx, umapParishes); err != nil {
			log.Printf("WARNING: failed to save parishes to Firestore: %v", err)
		} else {
			log.Printf("Synced %d parishes from uMap to Firestore", len(umapParishes))
			// Notify the web server to reload parishes
			reloadResp, err := http.Post("https://ortodoxagudstjanster.se/reload-parishes", "", nil)
			if err != nil {
				log.Printf("WARNING: failed to notify web server to reload parishes: %v", err)
			} else {
				reloadResp.Body.Close()
				log.Printf("Web server parish reload: HTTP %d", reloadResp.StatusCode)
			}
		}
	}

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
		gomosScraper.SetUploadSource(uploadReader, "st-georgios/")
	}
	registry.Register(gomosScraper)
	registry.Register(scraper.NewHeligaAnnaScraper())
	registry.Register(scraper.NewRyskaScraper(gcsStore, visionClient))
	registry.Register(scraper.NewHeligeSergijScraper(gcsStore, visionClient))
registry.Register(scraper.NewGCalendarScraper())
	registry.Register(scraper.NewGCalendarManualScraper())
	registry.Register(scraper.NewUppstandelseScraper())
	registry.Register(scraper.NewRomanianScraper())
	registry.Register(scraper.NewSommarlagerScraper(gcsStore, visionClient))
	if uploadReader != nil {
		uploadParishes := map[string]scraper.UploadParishInfo{
			"helige-giorgis": {
				Name:      "Helige Giorgis",
				Location:  "Helige Giorgis, Kyrkvägen 27, 182 74 Stocksund",
				SourceURL: "https://www.facebook.com/share/17oMW5H9UN/?mibextid=wwXIfr",
				SourceName: "Facebook",
			},
		}
		registry.Register(scraper.NewUploadsScraper(gcsStore, visionClient, uploadReader, gcsUploadBucket, uploadParishes))
	}

	// Generate batch ID for this ingestion run
	batchID := time.Now().UTC().Format("20060102-150405")
	log.Printf("Starting ingestion with batch ID: %s", batchID)

	today := time.Now().Format("2006-01-02")

	// Pass 1: Run scrapers and collect accepted results
	scrapers := registry.Scrapers()
	var accepted []acceptedResult
	failedScrapers := 0
	var scraperErrors []scraperFailure // collected for email alert

	for _, s := range scrapers {
		scraperName := s.Name()
		log.Printf("Running scraper: %s", scraperName)

		services, err := s.Fetch(ctx)

		// Collect diagnostic notes if the scraper supports them.
		var fetchNotes []string
		if sn, ok := s.(scraper.ScraperWithNotes); ok {
			fetchNotes = sn.FetchNotes()
		}

		if err != nil {
			log.Printf("ERROR: Scraper %s failed: %v", scraperName, err)
			failedScrapers++
			scraperErrors = append(scraperErrors, scraperFailure{name: scraperName, err: err, notes: fetchNotes})
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
			} else if newCount*3 < existingCount {
				log.Printf("WARNING: Scraper %s returned significantly fewer future services (%d) than currently stored (%d). Skipping replacement.",
					scraperName, newCount, existingCount)

				// Save rejected data to GCS for diagnostics
				gcsPath := saveDiagnostics(gcsStore, scraperName, services)

				// Send alert email if SMTP is configured
				if smtpConfig != nil {
					subject, body := buildCountDecreaseAlert(scraperName, existingCount, newCount, gcsBucket, gcsPath, services, fetchNotes)
					if err := smtpConfig.Send(subject, body); err != nil {
						log.Printf("ERROR: Failed to send alert email for %s: %v", scraperName, err)
					} else {
						log.Printf("Alert email sent for %s", scraperName)
					}
				}

				continue
			} else if newCount == 0 && existingCount == 0 && len(fetchNotes) > 0 {
				// All fetched events are past-dated and the scraper ran in degraded mode
				// (e.g. website down, using stale backup). The count-decrease check above
				// won't fire here because existingCount is also 0, so this is a blind spot.
				log.Printf("WARNING: Scraper %s returned %d events but none are future-dated (degraded mode)", scraperName, len(services))
				if smtpConfig != nil {
					subject, body := buildStaleFallbackAlert(scraperName, services, fetchNotes)
					if err := smtpConfig.Send(subject, body); err != nil {
						log.Printf("ERROR: Failed to send stale fallback alert for %s: %v", scraperName, err)
					} else {
						log.Printf("Stale fallback alert sent for %s", scraperName)
					}
				}
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
	unknownSlugs := make(map[string]string) // scraperName → first unknown slug
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
			// Set EventLanguage from parsed results, but only if detected and not already set
			if result.services[i].EventLanguage == nil {
				mapKey := eventLangMapKey(result.services[i].ServiceName, result.services[i].Occasion, result.services[i].Notes)
				if lang, ok := eventLangMap[mapKey]; ok && lang != nil {
					result.services[i].EventLanguage = lang
				}
			}
			if resolveParishFields(&result.services[i], result.scraperName, slugToParish, parishNameToSlug) {
				if _, seen := unknownSlugs[result.scraperName]; !seen {
					unknownSlugs[result.scraperName] = result.services[i].ParishSlug
				}
			}
		}

		fillConsecutiveEndTimes(result.services)

		if err := fsClient.ReplaceServicesForScraper(ctx, result.scraperName, result.services, batchID); err != nil {
			log.Printf("ERROR: Failed to store services for %s: %v", result.scraperName, err)
			failedScrapers++
			continue
		}
		log.Printf("Stored %d services for %s", len(result.services), result.scraperName)
		totalServices += len(result.services)
	}

	// Send consolidated alerts
	if smtpConfig != nil {
		if len(scraperErrors) > 0 {
			var lines []string
			for _, f := range scraperErrors {
				lines = append(lines, fmt.Sprintf("- %s: %v", f.name, f.err))
				for _, n := range f.notes {
					lines = append(lines, fmt.Sprintf("    • %s", n))
				}
			}
			body := "The following scrapers failed during ingestion:\r\n\r\n" + strings.Join(lines, "\r\n")
			if err := smtpConfig.Send("Ingestion alert: scrapers failed", body); err != nil {
				log.Printf("ERROR: Failed to send scraper failure alert: %v", err)
			} else {
				log.Printf("Alert email sent: %d scraper failure(s)", len(scraperErrors))
			}
		}
		if len(unknownSlugs) > 0 {
			var lines []string
			for scraper, slug := range unknownSlugs {
				lines = append(lines, fmt.Sprintf("- %s: slug %q", scraper, slug))
			}
			body := "The following scrapers have parish slugs that could not be resolved from uMap.\r\n" +
				"uMap data was available, so these are likely typos or stale slugs.\r\n" +
				"Falling back to scraper name as Parish for these scrapers.\r\n\r\n" +
				strings.Join(lines, "\r\n")
			if err := smtpConfig.Send("Ingestion alert: unknown parish slugs", body); err != nil {
				log.Printf("ERROR: Failed to send unknown slug alert: %v", err)
			} else {
				log.Printf("Alert email sent: %d unknown parish slug(s)", len(unknownSlugs))
			}
		}
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

type scraperFailure struct {
	name  string
	err   error
	notes []string
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

// fillConsecutiveEndTimes sets the end time of each service to the start time of
// the next service on the same day from the same parish, when no explicit end
// time exists and the gap is at most 3 hours (to avoid joining morning and evening
// services into a single multi-hour block).
func fillConsecutiveEndTimes(services []model.ChurchService) {
	type groupKey struct{ parish, date string }
	groups := make(map[groupKey][]*model.ChurchService)
	for i := range services {
		svc := &services[i]
		if svc.StartTime == nil {
			continue
		}
		k := groupKey{svc.Parish, svc.Date}
		groups[k] = append(groups[k], svc)
	}

	for _, group := range groups {
		sort.Slice(group, func(i, j int) bool {
			return group[i].StartTime.Before(*group[j].StartTime)
		})
		for i, svc := range group {
			if svc.EndTime != nil || i+1 >= len(group) {
				continue
			}
			next := group[i+1]
			if next.StartTime == nil {
				continue
			}
			gap := next.StartTime.Sub(*svc.StartTime)
			if gap > 0 && gap <= 3*time.Hour {
				end := *next.StartTime
				svc.EndTime = &end
			}
		}
	}
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

// buildCountDecreaseAlert formats the subject and body for a service count regression alert.
func buildCountDecreaseAlert(scraperName string, existingCount, newCount int, gcsBucket, gcsPath string, services []model.ChurchService, notes []string) (subject, body string) {
	today := time.Now().Format("2006-01-02")

	subject = fmt.Sprintf("Ingestion alert: %s – %d future events (was %d)", scraperName, newCount, existingCount)

	var sb strings.Builder
	fmt.Fprintf(&sb, "Scraper: %s\r\n", scraperName)
	fmt.Fprintf(&sb, "Rule: new future count (%d) < 1/3 of stored count (%d) → replacement skipped\r\n", newCount, existingCount)
	fmt.Fprintf(&sb, "Action: existing data preserved in Firestore\r\n")
	fmt.Fprintf(&sb, "\r\n")

	if len(services) == 0 {
		fmt.Fprintf(&sb, "Fetched events: none — the scraper returned no events at all.\r\n")
	} else {
		sorted := make([]model.ChurchService, len(services))
		copy(sorted, services)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date < sorted[j].Date })

		pastCount := len(services) - newCount
		fmt.Fprintf(&sb, "Fetched events: %d total (%d future, %d past/today)\r\n", len(services), newCount, pastCount)
		fmt.Fprintf(&sb, "Date range:     %s → %s\r\n", sorted[0].Date, sorted[len(sorted)-1].Date)
		fmt.Fprintf(&sb, "\r\n")

		shown := sorted
		truncated := false
		if len(shown) > 20 {
			shown = shown[:20]
			truncated = true
		}
		for _, svc := range shown {
			timeStr := "     "
			if svc.Time != nil {
				timeStr = fmt.Sprintf("%-5s", *svc.Time)
			}
			marker := ""
			if svc.Date >= today {
				marker = "  ← future"
			}
			fmt.Fprintf(&sb, "  %s  %s  %s%s\r\n", svc.Date, timeStr, svc.ServiceName, marker)
		}
		if truncated {
			fmt.Fprintf(&sb, "  … and %d more (see diagnostics)\r\n", len(services)-20)
		}
	}

	if len(notes) > 0 {
		fmt.Fprintf(&sb, "\r\n")
		fmt.Fprintf(&sb, "Scraper diagnostics:\r\n")
		for _, n := range notes {
			fmt.Fprintf(&sb, "  - %s\r\n", n)
		}
	}

	fmt.Fprintf(&sb, "\r\n")
	fmt.Fprintf(&sb, "Diagnostics file: gs://%s/%s\r\n", gcsBucket, gcsPath)

	return subject, sb.String()
}

// buildStaleFallbackAlert formats a warning email for when a scraper returns only
// past-dated events while running in degraded mode (e.g. website down, using backup).
func buildStaleFallbackAlert(scraperName string, services []model.ChurchService, notes []string) (subject, body string) {
	subject = fmt.Sprintf("Ingestion warning: %s – all %d fetched events are past-dated (degraded mode)", scraperName, len(services))

	var sb strings.Builder
	fmt.Fprintf(&sb, "Scraper: %s\r\n", scraperName)
	fmt.Fprintf(&sb, "Warning: scraper returned %d events but none are future-dated.\r\n", len(services))
	fmt.Fprintf(&sb, "This usually means the website is down and a stale backup is being used.\r\n")
	fmt.Fprintf(&sb, "No data was changed in Firestore.\r\n")

	if len(notes) > 0 {
		fmt.Fprintf(&sb, "\r\nScraper diagnostics:\r\n")
		for _, n := range notes {
			fmt.Fprintf(&sb, "  - %s\r\n", n)
		}
	}

	return subject, sb.String()
}

// resolveParishFields fills in missing Parish, ParishSlug, and ParishLanguage using
// the uMap data. Scrapers that set only ParishSlug get Parish and ParishLanguage resolved
// from uMap. Scrapers that set only Parish get ParishSlug resolved via reverse lookup,
// then ParishLanguage from uMap.
// scraperName is used as a Parish fallback when uMap is unavailable.
// Returns true if a slug was set but not found in uMap while uMap data was available.
func resolveParishFields(svc *model.ChurchService, scraperName string, slugToParish map[string]umap.Parish, nameToSlug map[string]string) bool {
	unknown := false
	if svc.ParishSlug != "" && svc.Parish == "" {
		if parish, ok := slugToParish[svc.ParishSlug]; ok {
			svc.Parish = parish.Name
		} else {
			// Use scraper name as fallback: for fixed scrapers it equals the canonical
			// parish name (unlike Source, which may be a calendar/feed name).
			svc.Parish = scraperName
			// Only signal unknown if uMap data was actually available.
			unknown = len(slugToParish) > 0
		}
	} else if svc.Parish != "" && svc.ParishSlug == "" {
		if slug, ok := nameToSlug[svc.Parish]; ok {
			svc.ParishSlug = slug
		}
	}

	// Set ParishLanguage from uMap if not already set by the scraper.
	if svc.ParishSlug != "" && svc.ParishLanguage == nil {
		if parish, ok := slugToParish[svc.ParishSlug]; ok {
			if lang := buildParishLanguage(parish.PrimaryLanguage, parish.SecondaryLanguages); lang != "" {
				svc.ParishLanguage = &lang
			}
		}
	}

	return unknown
}

// buildParishLanguage joins primary and secondary languages into a single display string.
func buildParishLanguage(primary string, secondary []string) string {
	parts := []string{}
	if primary != "" {
		parts = append(parts, primary)
	}
	for _, s := range secondary {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}
