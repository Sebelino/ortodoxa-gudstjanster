package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"ortodoxa-gudstjanster/internal/firestore"
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

	// Initialize HTTP handlers
	handler := web.New(fsClient)

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

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
