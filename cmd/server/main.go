package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"ortodoxa-gudstjanster/internal/cache"
	"ortodoxa-gudstjanster/internal/scraper"
	"ortodoxa-gudstjanster/internal/store"
	"ortodoxa-gudstjanster/internal/vision"
	"ortodoxa-gudstjanster/internal/web"
)

const defaultCacheTTL = 6 * time.Hour

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	cacheDir := os.Getenv("CACHE_DIR")
	if cacheDir == "" {
		cacheDir = "cache"
	}

	openaiAPIKey := os.Getenv("OPENAI_API_KEY")

	// Initialize cache
	c, err := cache.New(cacheDir, defaultCacheTTL)
	if err != nil {
		log.Fatalf("Failed to initialize cache: %v", err)
	}

	// Initialize store (GCS or local)
	var s store.Store
	gcsBucket := os.Getenv("GCS_BUCKET")
	if gcsBucket != "" {
		ctx := context.Background()
		gcsStore, err := store.NewGCS(ctx, gcsBucket)
		if err != nil {
			log.Fatalf("Failed to initialize GCS store: %v", err)
		}
		s = gcsStore
		log.Printf("Store: GCS bucket %s", gcsBucket)
	} else {
		storeDir := os.Getenv("STORE_DIR")
		if storeDir == "" {
			storeDir = "disk"
		}
		localStore, err := store.NewLocal(storeDir)
		if err != nil {
			log.Fatalf("Failed to initialize local store: %v", err)
		}
		s = localStore
		log.Printf("Store: local directory %s", storeDir)
	}

	// Initialize vision client
	visionClient := vision.NewClient(openaiAPIKey)

	// Initialize scraper registry and register all scrapers
	registry := scraper.NewRegistry()
	registry.Register(scraper.NewFinskaScraper(""))
	registry.Register(scraper.NewGomosScraper(s, visionClient))
	registry.Register(scraper.NewHeligaAnnaScraper())
	registry.Register(scraper.NewRyskaScraper(s, visionClient))
	registry.Register(scraper.NewSrpskaScraper())

	// Initialize HTTP handlers
	handler := web.New(registry, c)

	// Configure SMTP if environment variables are set
	if smtpHost := os.Getenv("SMTP_HOST"); smtpHost != "" {
		handler.SetSMTP(&web.SMTPConfig{
			Host:     smtpHost,
			Port:     os.Getenv("SMTP_PORT"),
			User:     os.Getenv("SMTP_USER"),
			Password: os.Getenv("SMTP_PASS"),
			To:       os.Getenv("SMTP_TO"),
		})
		log.Printf("SMTP configured: %s -> %s", os.Getenv("SMTP_USER"), os.Getenv("SMTP_TO"))
	} else {
		log.Printf("SMTP not configured (feedback emails disabled)")
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	log.Printf("Server starting on port %s", port)
	log.Printf("Cache directory: %s", cacheDir)
	log.Printf("Registered scrapers: %d", len(registry.Scrapers()))

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
