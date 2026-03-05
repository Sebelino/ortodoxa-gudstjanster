//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
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

	sort.Slice(services, func(i, j int) bool {
		ti := services[i].Title
		if ti == "" {
			ti = services[i].ServiceName
		}
		tj := services[j].Title
		if tj == "" {
			tj = services[j].ServiceName
		}
		return ti < tj
	})

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "TITLE\tSERVICE_NAME\n")
	fmt.Fprintf(w, "-----\t------------\n")
	for _, s := range services {
		title := s.Title
		if title == "" {
			title = "(none)"
		}
		fmt.Fprintf(w, "%s\t%s\n", title, s.ServiceName)
	}
	w.Flush()
}
