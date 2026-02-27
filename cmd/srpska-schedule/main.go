// Convenience wrapper that combines Part 1 (fetch) and Part 2 (parse).
// Fetches the schedule table and outputs structured JSON.
//
// Usage: CHROME_PATH=/path/to/chromium go run ./cmd/srpska-schedule
//
// Equivalent to: go run ./cmd/srpska-fetch | go run ./cmd/srpska-parse
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"ortodoxa-gudstjanster/internal/srpska"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Part 1: Fetch raw table text
	tableText, err := srpska.FetchScheduleTable(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching: %v\n", err)
		os.Exit(1)
	}

	// Part 2: Parse into structured schedule
	schedule, err := srpska.ParseScheduleTable(tableText)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing: %v\n", err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(schedule); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		os.Exit(1)
	}
}
