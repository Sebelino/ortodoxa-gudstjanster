// Part 2: Parse raw table text into structured recurring schedule JSON.
// Reads raw table text from stdin, outputs structured JSON to stdout.
//
// Usage: cat raw.txt | go run ./cmd/srpska-parse
// Or:    go run ./cmd/srpska-fetch | go run ./cmd/srpska-parse
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"ortodoxa-gudstjanster/internal/srpska"
)

func main() {
	// Read raw table text from stdin
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
		os.Exit(1)
	}

	schedule, err := srpska.ParseScheduleTable(string(input))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(schedule); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		os.Exit(1)
	}
}
