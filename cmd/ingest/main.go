package main

import (
	"context"
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

	// Generate batch ID for this ingestion run
	batchID := time.Now().UTC().Format("20060102-150405")
	log.Printf("Starting ingestion with batch ID: %s", batchID)

	// Run each scraper sequentially
	scrapers := registry.Scrapers()
	totalServices := 0
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
			existingCount, err := fsClient.CountServicesForSource(ctx, scraperName)
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

			if err := fsClient.ReplaceServicesForSource(ctx, scraperName, services, batchID); err != nil {
				log.Printf("ERROR: Failed to store services for %s: %v", scraperName, err)
				failedScrapers++
				continue
			}
			log.Printf("Stored %d services for %s", len(services), scraperName)
			totalServices += len(services)
		}
	}

	log.Printf("Ingestion complete. Total services: %d, Failed scrapers: %d/%d",
		totalServices, failedScrapers, len(scrapers))

	if failedScrapers > 0 {
		os.Exit(1)
	}
	fmt.Println("Ingestion completed successfully")
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
