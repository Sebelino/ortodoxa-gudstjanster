//go:build ignore

package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

func main() {
	source := flag.String("source", "", "Filter by source/parish name")
	date := flag.String("date", "", "Filter by date (YYYY-MM-DD)")
	serviceName := flag.String("service-name", "", "Filter by service_name (substring match)")
	docID := flag.String("id", "", "Delete a specific document by ID")
	dry := flag.Bool("dry", false, "Dry run: show matching documents without deleting")
	project := flag.String("project", "ortodoxa-gudstjanster", "GCP project ID")
	collection := flag.String("collection", "services", "Firestore collection name")
	flag.Parse()

	if *source == "" && *date == "" && *serviceName == "" && *docID == "" {
		log.Fatal("At least one filter is required: -source, -date, -service-name, or -id")
	}

	ctx := context.Background()
	client, err := firestore.NewClient(ctx, *project)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	coll := client.Collection(*collection)

	// If deleting by specific document ID
	if *docID != "" {
		docRef := coll.Doc(*docID)
		snap, err := docRef.Get(ctx)
		if err != nil {
			log.Fatalf("Document %s not found: %v", *docID, err)
		}
		data := snap.Data()
		fmt.Printf("Document: %s\n", snap.Ref.ID)
		fmt.Printf("  date:         %v\n", data["date"])
		fmt.Printf("  service_name: %v\n", data["service_name"])
		fmt.Printf("  source:       %v\n", data["source"])
		fmt.Printf("  time:         %v\n", data["time"])
		if *dry {
			fmt.Println("\nDry run: would delete 1 document")
			return
		}
		if _, err := docRef.Delete(ctx); err != nil {
			log.Fatal(err)
		}
		fmt.Println("\nDeleted 1 document")
		return
	}

	// Build query with available filters
	var query firestore.Query
	query = coll.Query
	if *source != "" {
		query = query.Where("source", "==", *source)
	}
	if *date != "" {
		query = query.Where("date", "==", *date)
	}

	iter := query.Documents(ctx)
	var matches []*firestore.DocumentSnapshot
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		// Client-side substring filter for service_name
		if *serviceName != "" {
			name, _ := doc.Data()["service_name"].(string)
			if name == "" || !contains(name, *serviceName) {
				continue
			}
		}
		matches = append(matches, doc)
	}

	if len(matches) == 0 {
		fmt.Println("No matching documents found")
		return
	}

	for _, doc := range matches {
		data := doc.Data()
		fmt.Printf("Document: %s\n", doc.Ref.ID)
		fmt.Printf("  date:         %v\n", data["date"])
		fmt.Printf("  service_name: %v\n", data["service_name"])
		fmt.Printf("  source:       %v\n", data["source"])
		fmt.Printf("  time:         %v\n", data["time"])
		fmt.Println()
	}

	if *dry {
		fmt.Printf("Dry run: would delete %d document(s)\n", len(matches))
		return
	}

	batch := client.Batch()
	for _, doc := range matches {
		batch.Delete(doc.Ref)
	}
	if _, err := batch.Commit(ctx); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Deleted %d document(s)\n", len(matches))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
