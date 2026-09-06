// Script to add a correction to Firestore.
//
// Usage (override time):
//
//	go run scripts/add-correction.go \
//	  -parish-slug=sankt-goran \
//	  -date=2026-08-06 \
//	  -original-time="08:30 - 12:30" \
//	  -time="09:30 - 12:30" \
//	  -reason="Start time corrected from 08:30 to 09:30"
//
// Usage (delete event):
//
//	go run scripts/add-correction.go \
//	  -parish-slug=ukrainska-ortodoxa-stockholm \
//	  -date=2026-09-07 \
//	  -original-time="09:00" \
//	  -service-name="Bön" \
//	  -delete \
//	  -reason="Not relevant to parish"
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"

	"cloud.google.com/go/firestore"
)

func main() {
	projectID := flag.String("project", "ortodoxa-gudstjanster", "GCP project ID")
	parishSlug := flag.String("parish-slug", "", "Parish slug (required)")
	date := flag.String("date", "", "Event date YYYY-MM-DD (required)")
	originalTime := flag.String("original-time", "", "Original time to match (required)")
	serviceName := flag.String("service-name", "", "Service name to match (optional, narrows match)")
	newTime := flag.String("time", "", "New time value (optional)")
	deleteFlag := flag.Bool("delete", false, "Delete the matched event instead of correcting it")
	reason := flag.String("reason", "", "Correction reason (required)")
	flag.Parse()

	if *parishSlug == "" || *date == "" || *reason == "" {
		flag.Usage()
		os.Exit(1)
	}

	ctx := context.Background()
	client, err := firestore.NewClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer client.Close()

	data := map[string]interface{}{
		"parish_slug":   *parishSlug,
		"date":          *date,
		"original_time": *originalTime,
		"reason":        *reason,
	}
	if *serviceName != "" {
		data["service_name"] = *serviceName
	}
	if *newTime != "" {
		data["time"] = *newTime
	}
	if *deleteFlag {
		data["delete"] = true
	}

	// Document ID from parish_slug + date + original_time + service_name
	hashInput := *parishSlug + "|" + *date + "|" + *originalTime
	if *serviceName != "" {
		hashInput += "|" + *serviceName
	}
	hash := sha256.Sum256([]byte(hashInput))
	docID := hex.EncodeToString(hash[:16])

	_, err = client.Collection("corrections").Doc(docID).Set(ctx, data)
	if err != nil {
		log.Fatalf("Failed to write correction: %v", err)
	}

	fmt.Printf("Correction saved (doc ID: %s)\n", docID)
	fmt.Printf("  Parish:  %s\n", *parishSlug)
	fmt.Printf("  Date:    %s\n", *date)
	if *serviceName != "" {
		fmt.Printf("  Service: %s\n", *serviceName)
	}
	if *deleteFlag {
		fmt.Printf("  Action:  DELETE\n")
	} else {
		fmt.Printf("  Time:    %s → %s\n", *originalTime, *newTime)
	}
	fmt.Printf("  Reason:  %s\n", *reason)
}
