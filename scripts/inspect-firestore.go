//go:build ignore

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

func main() {
	projectID := flag.String("project", "ortodoxa-gudstjanster", "GCP project ID")
	collection := flag.String("collection", "services", "Firestore collection name")
	source := flag.String("source", "", "Filter by source (optional)")
	limit := flag.Int("limit", 10, "Max documents to return (0 for all)")
	countOnly := flag.Bool("count", false, "Only show counts per source")
	flag.Parse()

	ctx := context.Background()
	client, err := firestore.NewClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer client.Close()

	coll := client.Collection(*collection)

	if *countOnly {
		showCounts(ctx, coll)
		return
	}

	var query firestore.Query = coll.Query
	if *source != "" {
		query = coll.Where("source", "==", *source)
	}
	if *limit > 0 {
		query = query.Limit(*limit)
	}

	iter := query.Documents(ctx)
	count := 0
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Fatalf("Error iterating documents: %v", err)
		}

		data := doc.Data()
		jsonData, _ := json.MarshalIndent(data, "", "  ")
		fmt.Printf("--- Document: %s ---\n%s\n\n", doc.Ref.ID, string(jsonData))
		count++
	}

	fmt.Printf("Total documents shown: %d\n", count)
}

func showCounts(ctx context.Context, coll *firestore.CollectionRef) {
	counts := make(map[string]int)
	total := 0

	iter := coll.Documents(ctx)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Fatalf("Error iterating documents: %v", err)
		}

		data := doc.Data()
		source, _ := data["source"].(string)
		counts[source]++
		total++
	}

	fmt.Println("Services per source:")
	fmt.Println("--------------------")
	for source, count := range counts {
		fmt.Printf("%-45s %d\n", source, count)
	}
	fmt.Println("--------------------")
	fmt.Printf("%-45s %d\n", "TOTAL", total)
}
