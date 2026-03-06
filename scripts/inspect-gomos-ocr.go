//go:build ignore

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"text/tabwriter"

	"cloud.google.com/go/storage"

	"ortodoxa-gudstjanster/internal/vision"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: go run scripts/inspect-gomos-ocr.go <image-checksum-or-file-path>\n")
		os.Exit(1)
	}

	arg := os.Args[1]
	checksum := arg

	// If the argument looks like a file path, read it and compute checksum
	if _, err := os.Stat(arg); err == nil {
		data, err := os.ReadFile(arg)
		if err != nil {
			log.Fatalf("Failed to read file %s: %v", arg, err)
		}
		hash := sha256.Sum256(data)
		checksum = hex.EncodeToString(hash[:])
		fmt.Printf("File: %s\nChecksum: %s\n\n", arg, checksum)
	}

	bucket := os.Getenv("GCS_BUCKET")
	if bucket == "" {
		bucket = "ortodoxa-gudstjanster-ortodoxa-store"
	}

	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create GCS client: %v", err)
	}
	defer client.Close()

	key := "gomos-ocr/v1/" + checksum + ".json"
	reader, err := client.Bucket(bucket).Object(key).NewReader(ctx)
	if err != nil {
		log.Fatalf("Failed to read %s from bucket %s: %v", key, bucket, err)
	}
	defer reader.Close()

	var entry struct {
		Language string                 `json:"language"`
		Entries  []vision.ScheduleEntry `json:"entries"`
	}
	if err := json.NewDecoder(reader).Decode(&entry); err != nil {
		log.Fatalf("Failed to decode JSON: %v", err)
	}

	fmt.Printf("Language: %s\n", entry.Language)
	fmt.Printf("Entries:  %d\n\n", len(entry.Entries))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "DATE\tDAY\tTIME\tSERVICE\tOCCASION")
	fmt.Fprintln(w, "----\t---\t----\t-------\t--------")
	for _, e := range entry.Entries {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", e.Date, e.DayOfWeek, e.Time, e.ServiceName, e.Occasion)
	}
	w.Flush()
}
