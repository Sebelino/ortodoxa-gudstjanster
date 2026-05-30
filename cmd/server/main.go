package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"

	"ortodoxa-gudstjanster/internal/email"
	"ortodoxa-gudstjanster/internal/firestore"
	"ortodoxa-gudstjanster/internal/umap"
	"ortodoxa-gudstjanster/internal/web"
)

func main() {
	ctx := context.Background()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		log.Fatal("GCP_PROJECT_ID environment variable is required")
	}

	firestoreCollection := os.Getenv("FIRESTORE_COLLECTION")
	if firestoreCollection == "" {
		firestoreCollection = "services"
	}

	// Initialize Firestore client
	fsClient, err := firestore.New(ctx, projectID, firestoreCollection)
	if err != nil {
		log.Fatalf("Failed to initialize Firestore client: %v", err)
	}
	defer fsClient.Close()
	log.Printf("Firestore: project %s, collection %s", projectID, firestoreCollection)

	// Load parishes: try uMap first, fall back to Firestore
	parishes, err := umap.FetchParishes()
	if err != nil {
		log.Printf("WARNING: failed to fetch parishes from uMap: %v", err)
		log.Printf("Falling back to Firestore parish cache")
		parishes, err = fsClient.GetParishes(ctx)
		if err != nil {
			log.Fatalf("Failed to load parishes from Firestore: %v", err)
		}
	} else {
		// Sync to Firestore for future fallback
		if err := fsClient.SaveParishes(ctx, parishes); err != nil {
			log.Printf("WARNING: failed to cache parishes in Firestore: %v", err)
		}
	}
	if len(parishes) == 0 {
		log.Fatal("No parishes loaded from uMap or Firestore")
	}
	log.Printf("Loaded %d parishes", len(parishes))
	web.SetParishes(parishes)

	// Initialize HTTP handlers
	handler := web.New(fsClient)

	// Configure SMTP if environment variables are set
	if smtpHost := strings.TrimSpace(os.Getenv("SMTP_HOST")); smtpHost != "" {
		handler.SetSMTP(&email.SMTPConfig{
			Host:     smtpHost,
			Port:     strings.TrimSpace(os.Getenv("SMTP_PORT")),
			User:     strings.TrimSpace(os.Getenv("SMTP_USER")),
			Password: strings.TrimSpace(os.Getenv("SMTP_PASS")),
			To:       strings.TrimSpace(os.Getenv("SMTP_TO")),
		})
		log.Printf("SMTP configured: %s -> %s", os.Getenv("SMTP_USER"), os.Getenv("SMTP_TO"))
	} else {
		log.Printf("SMTP not configured (feedback emails disabled)")
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	log.Printf("Server starting on port %s", port)

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
