// Part 1: Fetch raw table text from the Sankt Sava website using chromedp.
// Outputs the raw table text to stdout.
//
// Usage: CHROME_PATH=/path/to/chromium go run ./cmd/srpska-fetch
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"ortodoxa-gudstjanster/internal/srpska"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tableText, err := srpska.FetchScheduleTable(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Print(tableText)
}
