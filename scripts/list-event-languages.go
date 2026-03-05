//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	fs "ortodoxa-gudstjanster/internal/firestore"
)

func main() {
	ctx := context.Background()

	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		projectID = "ortodoxa-gudstjanster"
	}
	collection := os.Getenv("FIRESTORE_COLLECTION")
	if collection == "" {
		collection = "services"
	}

	client, err := fs.New(ctx, projectID, collection)
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer client.Close()

	services, err := client.GetAllServices(ctx)
	if err != nil {
		log.Fatalf("Failed to get services: %v", err)
	}

	type row struct {
		lang    string
		details string
	}

	var rows []row
	for _, s := range services {
		parts := []string{s.ServiceName}
		if s.Notes != nil && *s.Notes != "" {
			parts = append(parts, *s.Notes)
		}
		if s.Occasion != nil && *s.Occasion != "" {
			parts = append(parts, *s.Occasion)
		}
		details := strings.Join(parts, " ")

		lang := ""
		if s.EventLanguage != nil {
			lang = *s.EventLanguage
		}
		rows = append(rows, row{lang: lang, details: details})
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].lang < rows[j].lang
	})

	w := tabwriter.NewWriter(os.Stdout, 16, 0, 2, ' ', 0)
	fmt.Fprintf(w, "EVENT_LANGUAGE\tDETAILS\n")
	fmt.Fprintf(w, "--------------\t-------\n")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\n", r.lang, r.details)
	}
	w.Flush()
}
