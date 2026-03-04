package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: go run scripts/delete-source.go <source-name>")
	}
	source := os.Args[1]

	ctx := context.Background()
	client, err := firestore.NewClient(ctx, "ortodoxa-gudstjanster")
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	iter := client.Collection("services").Where("source", "==", source).Documents(ctx)
	batch := client.Batch()
	count := 0
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		batch.Delete(doc.Ref)
		count++
	}
	if count > 0 {
		if _, err := batch.Commit(ctx); err != nil {
			log.Fatal(err)
		}
	}
	fmt.Printf("Deleted %d documents for source %q\n", count, source)
}
