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
	registry.Register(scraper.NewDemetriosScraper())
	registry.Register(scraper.NewGCalendarScraper())
	registry.Register(scraper.NewGCalendarManualScraper())
	registry.Register(scraper.NewUppstandelseScraper())
	registry.Register(scraper.NewRomanianScraper())
	registry.Register(scraper.NewUkrainskaScraper(gcsStore, visionClient))
	registry.Register(scraper.NewSommarlagerScraper(gcsStore, visionClient))
	registry.Register(scraper.NewSigfridScraper())
	registry.Register(scraper.NewRumanskaLinkopingScraper())
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
			// Compare future service counts to detect regressions.
			// Scrapers that implement AllowDecreaser skip this check
			// (e.g. user-curated Google Calendar scrapers).
			allowDecrease := false
			if ad, ok := s.(scraper.AllowDecreaser); ok {
				allowDecrease = ad.AllowDecrease()
			}
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
			} else if !allowDecrease && newCount*3 < existingCount {
				log.Printf("WARNING: Scraper %s returned significantly fewer future services (%d) than currently stored (%d). Skipping replacement.",
					scraperName, newCount, existingCount)

				// Save rejected data to GCS for diagnostics
				gcsPath := saveDiagnostics(gcsStore, scraperName, services)

				// Send alert email if SMTP is configured and the rejection is
				// actually urgent: either a technical failure was detected,
				// or the currently-published schedule is close enough to
				// running dry that it matters — see shouldAlertOnCountDecrease.
				// A rejection that's just "nothing new posted yet" with
				// plenty of schedule still ahead stays silent.
				if smtpConfig != nil {
					hasFailureNote := notesIndicateFailure(fetchNotes)
					latestFutureDate, dateErr := fsClient.LatestFutureServiceDate(ctx, scraperName)
					if dateErr != nil {
						log.Printf("WARNING: failed to determine schedule runway for %s: %v", scraperName, dateErr)
					}
					if !shouldAlertOnCountDecrease(hasFailureNote, latestFutureDate) {
						log.Printf("Alert email skipped for %s: rejection not urgent (no failure detected, schedule runs through %s)", scraperName, latestFutureDate)
					} else if shouldSendCountDecreaseAlert(gcsStore, scraperName, services) {
						subject, body := buildCountDecreaseAlert(scraperName, existingCount, newCount, gcsBucket, gcsPath, services, fetchNotes, hasFailureNote, latestFutureDate)
						if err := smtpConfig.Send(subject, body); err != nil {
							log.Printf("ERROR: Failed to send alert email for %s: %v", scraperName, err)
						} else {
							log.Printf("Alert email sent for %s", scraperName)
						}
					} else {
						log.Printf("Alert email skipped for %s: rejection unchanged since last alert", scraperName)
					}
				}

				continue
			}

			accepted = append(accepted, acceptedResult{scraperName: scraperName, services: services})
		}
	}

	// Apply corrections from Firestore
	corrections, corrErr := fsClient.GetCorrections(ctx)
	if corrErr != nil {
		log.Printf("WARNING: failed to load corrections: %v", corrErr)
	} else if len(corrections) > 0 {
		applied := applyCorrections(accepted, corrections)
		log.Printf("Corrections: %d loaded, %d applied", len(corrections), applied)
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
			// Only alert when at least one failure is new or has changed
			// since it was last reported — an ongoing, unchanged outage
			// (e.g. an upstream site still down) doesn't get re-alerted
			// every 3 hours. Every currently-failing scraper is still
			// listed in the body for context once an alert does fire.
			anyNew := false
			for _, f := range scraperErrors {
				if shouldSendScraperFailureAlert(gcsStore, f.name, f.err.Error()) {
					anyNew = true
				}
			}
			if anyNew {
				subject, body := buildScraperFailureAlert(scraperErrors)
				if err := smtpConfig.Send(subject, body); err != nil {
					log.Printf("ERROR: Failed to send scraper failure alert: %v", err)
				} else {
					log.Printf("Alert email sent: %d scraper failure(s)", len(scraperErrors))
				}
			} else {
				log.Printf("Alert email skipped: %d scraper failure(s) unchanged since last alert", len(scraperErrors))
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

// safeScraperName sanitizes a scraper name for use as a path segment.
func safeScraperName(scraperName string) string {
	return strings.ReplaceAll(strings.ToLower(scraperName), " ", "-")
}

// lastAlertRecord is what's persisted per dedup key to suppress repeat
// alerts describing the same underlying problem (see shouldSendDedupedAlert).
// SentAt is purely informational (visible to anyone inspecting the file
// directly) except for driving periodic reminders — it plays no part in the
// "is this the same problem" decision itself.
type lastAlertRecord struct {
	Checksum string    `json:"checksum"`
	SentAt   time.Time `json:"sent_at"`
}

func countDecreaseAlertDedupKey(scraperName string) string {
	// No ".json" suffix here — GetJSON/SetJSON already append one.
	return fmt.Sprintf("diagnostics/%s/last-alert", safeScraperName(scraperName))
}

func scraperFailureAlertDedupKey(scraperName string) string {
	return fmt.Sprintf("diagnostics/%s/last-failure-alert", safeScraperName(scraperName))
}

// scraperFailureReminderInterval is how often a still-unresolved problem
// gets re-alerted even though nothing about it has changed — a hard scraper
// failure, or an urgent count-decrease rejection (see
// shouldAlertOnCountDecrease). Worth not losing track of if it drags on.
const scraperFailureReminderInterval = 24 * time.Hour

// countDecreaseRunwayThreshold: for a count-decrease rejection with no
// detected technical failure (see notesIndicateFailure) — most likely just a
// source that hasn't posted anything new — alerting is held off until the
// currently-published schedule is actually about to run dry, rather than on
// a fixed timer unrelated to it. A scraper that's been stuck for weeks is
// harmless as long as there's still a comfortable amount of already-known
// future schedule ahead; it only becomes a real problem once that runway
// gets this short with nothing new having shown up to extend it.
const countDecreaseRunwayThreshold = 5 * 24 * time.Hour

// notesIndicateFailure reports whether any diagnostic note logged during a
// Fetch call describes an actual technical failure (an OCR/fetch/parse
// error, etc.) as opposed to a routine "nothing new" note. Scrapers
// consistently word failure notes with "fail" (e.g. "OCR failed for image
// 2: ..."), so that's used as the signal.
func notesIndicateFailure(notes []string) bool {
	for _, n := range notes {
		if strings.Contains(strings.ToLower(n), "fail") {
			return true
		}
	}
	return false
}

// shouldAlertOnCountDecrease reports whether a count-decrease rejection is
// urgent enough to alert on at all: either the notes show a detected
// technical failure, or the currently-published schedule is close enough to
// running dry (see countDecreaseRunwayThreshold) that nothing new having
// turned up is itself a live problem. latestFutureDate is the latest date
// (YYYY-MM-DD) among the scraper's currently stored future services, or ""
// if unknown/none.
func shouldAlertOnCountDecrease(hasFailureNote bool, latestFutureDate string) bool {
	if hasFailureNote {
		return true
	}
	if latestFutureDate == "" {
		return true // nothing future stored at all — already as dry as it gets
	}
	lastDate, err := time.Parse("2006-01-02", latestFutureDate)
	if err != nil {
		return false
	}
	return time.Until(lastDate) <= countDecreaseRunwayThreshold
}

// shouldSendDedupedAlert reports whether an alert should actually be emailed,
// given a checksum of the content it would describe, persisted under key.
// The goal is to alert only when something changes — a scraper starting to
// fail or reject data when it wasn't before, or a failure/rejection changing
// shape — not to re-notify for an unchanged, already-known problem on every
// ingestion cycle. maxAge, if nonzero, additionally forces a reminder once
// that long has passed since the last alert, even with the content
// unchanged; maxAge == 0 means no periodic reminder at all — silent until
// the content actually changes. When it returns true, it also persists the
// new checksum and timestamp so the next call can compare against them.
func shouldSendDedupedAlert(gcsStore *store.GCSStore, key string, content []byte, maxAge time.Duration) bool {
	sum := sha256.Sum256(content)
	checksum := hex.EncodeToString(sum[:])

	var last lastAlertRecord
	if gcsStore.GetJSON(key, &last) {
		unchanged := last.Checksum == checksum
		reminderDue := maxAge > 0 && time.Since(last.SentAt) >= maxAge
		if unchanged && !reminderDue {
			return false
		}
	}

	record := lastAlertRecord{Checksum: checksum, SentAt: time.Now().UTC()}
	if err := gcsStore.SetJSON(key, record); err != nil {
		log.Printf("WARNING: failed to save alert dedup record for %s: %v", key, err)
	}
	return true
}

// shouldSendCountDecreaseAlert is shouldSendDedupedAlert specialized for
// count-decrease rejections, checksumming the rejected services themselves.
// Only call this once shouldAlertOnCountDecrease has already decided the
// rejection is urgent — an unchanged, non-urgent rejection isn't tracked at
// all, so it starts fresh (alerts immediately) the moment it does become
// urgent rather than waiting out a reminder cadence that was never running.
func shouldSendCountDecreaseAlert(gcsStore *store.GCSStore, scraperName string, services []model.ChurchService) bool {
	data, err := json.Marshal(services)
	if err != nil {
		log.Printf("WARNING: failed to checksum rejected data for %s: %v", scraperName, err)
	}
	return shouldSendDedupedAlert(gcsStore, countDecreaseAlertDedupKey(scraperName), data, scraperFailureReminderInterval)
}

// shouldSendScraperFailureAlert is shouldSendDedupedAlert specialized for
// scraper failures, checksumming the error message. A new or different error
// always alerts; an unchanged, still-failing error gets a daily reminder
// (scraperFailureReminderInterval) rather than going silent indefinitely.
func shouldSendScraperFailureAlert(gcsStore *store.GCSStore, scraperName, errMsg string) bool {
	return shouldSendDedupedAlert(gcsStore, scraperFailureAlertDedupKey(scraperName), []byte(errMsg), scraperFailureReminderInterval)
}

// saveDiagnostics serializes rejected services to GCS and returns the object path.
func saveDiagnostics(gcsStore *store.GCSStore, scraperName string, services []model.ChurchService) string {
	timestamp := time.Now().UTC().Format("20060102-150405")
	path := fmt.Sprintf("diagnostics/%s/%s.json", safeScraperName(scraperName), timestamp)

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

// buildScraperFailureAlert formats the subject and body for one or more
// scrapers that errored out entirely this run (as opposed to a count
// decrease, where the scraper did return data but less of it than expected).
// The subject names the affected scraper(s) directly, and the body leads
// with a plain-language summary before the technical error, so the actual
// problem is clear without needing to decode a raw Go error string.
func buildScraperFailureAlert(failures []scraperFailure) (subject, body string) {
	if len(failures) == 1 {
		subject = fmt.Sprintf("Ingestion alert: %s failed to update", failures[0].name)
	} else {
		names := make([]string, len(failures))
		for i, f := range failures {
			names[i] = f.name
		}
		subject = fmt.Sprintf("Ingestion alert: %d scrapers failed to update (%s)", len(failures), strings.Join(names, ", "))
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d scraper(s) could not fetch their schedule this run and were skipped.\r\n", len(failures))
	fmt.Fprintf(&sb, "The site's existing published data for them is unaffected — nothing was changed or removed.\r\n")
	fmt.Fprintf(&sb, "\r\n")

	for i, f := range failures {
		fmt.Fprintf(&sb, "%s\r\n", f.name)
		fmt.Fprintf(&sb, "  Error: %v\r\n", f.err)
		for _, n := range f.notes {
			fmt.Fprintf(&sb, "    • %s\r\n", n)
		}
		if i < len(failures)-1 {
			fmt.Fprintf(&sb, "\r\n")
		}
	}

	fmt.Fprintf(&sb, "\r\n---\r\n")
	fmt.Fprintf(&sb, "This alert fires on a new or changed error, and once a day as a reminder for as long as it stays unresolved.\r\n")

	return subject, sb.String()
}

// buildCountDecreaseAlert formats the subject and body for a service count
// regression alert. hasFailureNote and latestFutureDate explain why this
// particular rejection was judged urgent enough to alert on — see
// shouldAlertOnCountDecrease.
func buildCountDecreaseAlert(scraperName string, existingCount, newCount int, gcsBucket, gcsPath string, services []model.ChurchService, notes []string, hasFailureNote bool, latestFutureDate string) (subject, body string) {
	today := time.Now().Format("2006-01-02")

	subject = fmt.Sprintf("Ingestion alert: %s – %d future events (was %d)", scraperName, newCount, existingCount)

	var sb strings.Builder
	fmt.Fprintf(&sb, "Scraper: %s\r\n", scraperName)
	fmt.Fprintf(&sb, "Rule: new future count (%d) < 1/3 of stored count (%d) → replacement skipped\r\n", newCount, existingCount)
	fmt.Fprintf(&sb, "Action: existing data preserved in Firestore\r\n")
	if hasFailureNote {
		fmt.Fprintf(&sb, "Why now: a technical failure was detected while fetching (see diagnostics below)\r\n")
	} else if latestFutureDate == "" {
		fmt.Fprintf(&sb, "Why now: no future events remain in the currently published schedule\r\n")
	} else {
		fmt.Fprintf(&sb, "Why now: the currently published schedule only runs through %s and nothing new has replaced it\r\n", latestFutureDate)
	}
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

// applyCorrections matches corrections to scraped services and overrides fields.
// Corrections match on parish_slug + date + time, optionally narrowed by
// service_name. A correction with Delete=true removes the event entirely.
func applyCorrections(accepted []acceptedResult, corrections []model.Correction) int {
	// Build lookup keyed by (slug, date, time). Multiple corrections can
	// target the same slot if they differ by service_name.
	type corrKey struct{ slug, date, time string }
	lookup := make(map[corrKey][]model.Correction)
	for _, c := range corrections {
		k := corrKey{c.ParishSlug, c.Date, c.OriginalTime}
		lookup[k] = append(lookup[k], c)
	}

	applied := 0
	for ri := range accepted {
		kept := accepted[ri].services[:0]
		for i := range accepted[ri].services {
			svc := &accepted[ri].services[i]
			timeStr := ""
			if svc.Time != nil {
				timeStr = *svc.Time
			}
			corrs, ok := lookup[corrKey{svc.ParishSlug, svc.Date, timeStr}]
			if !ok {
				kept = append(kept, *svc)
				continue
			}
			// Find the matching correction. If a correction specifies
			// ServiceName it only matches that exact name; otherwise it
			// matches any event in the slot.
			var matched *model.Correction
			for j := range corrs {
				if corrs[j].ServiceName != "" && corrs[j].ServiceName != svc.ServiceName {
					continue
				}
				matched = &corrs[j]
				break
			}
			if matched == nil {
				kept = append(kept, *svc)
				continue
			}
			if matched.Delete {
				applied++
				log.Printf("Deleted %s %s %s %q: %s", svc.ParishSlug, svc.Date, timeStr, svc.ServiceName, matched.Reason)
				continue // skip — don't append to kept
			}
			if matched.Time != "" {
				svc.Time = &matched.Time
			}
			svc.Correction = &matched.Reason
			applied++
			log.Printf("Applied correction to %s %s %s: %s", svc.ParishSlug, svc.Date, timeStr, matched.Reason)
			kept = append(kept, *svc)
		}
		accepted[ri].services = kept
	}
	return applied
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
