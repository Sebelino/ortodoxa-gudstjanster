// Part 3: Generate calendar events from structured recurring schedule JSON.
// Reads schedule JSON from stdin, outputs calendar events JSON to stdout.
//
// Usage: cat schedule.json | go run ./cmd/srpska-generate
// Or:    go run ./cmd/srpska-schedule | go run ./cmd/srpska-generate
// Or:    go run ./cmd/srpska-fetch | go run ./cmd/srpska-parse | go run ./cmd/srpska-generate
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"ortodoxa-gudstjanster/internal/srpska"
)

const defaultWeeks = 26

func main() {
	// Read schedule JSON from stdin
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
		os.Exit(1)
	}

	var schedule srpska.RecurringSchedule
	if err := json.Unmarshal(input, &schedule); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing JSON: %v\n", err)
		os.Exit(1)
	}

	// Generate events for 8 weeks
	events := srpska.GenerateEvents(&schedule, defaultWeeks)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(events); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		os.Exit(1)
	}
}
