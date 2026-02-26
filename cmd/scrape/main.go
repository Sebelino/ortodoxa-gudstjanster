package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/scraper"
	"ortodoxa-gudstjanster/internal/store"
	"ortodoxa-gudstjanster/internal/vision"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	var all []model.ChurchService

	// Finska
	finska := scraper.NewFinskaScraper("")
	if services, err := finska.Fetch(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "finska: %v\n", err)
	} else {
		all = append(all, services...)
	}

	// Gomos
	storeDir := os.Getenv("STORE_DIR")
	if storeDir == "" {
		storeDir = "disk"
	}
	s, err := store.New(storeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "store: %v\n", err)
		os.Exit(1)
	}
	visionClient := vision.NewClient(os.Getenv("OPENAI_API_KEY"))

	gomos := scraper.NewGomosScraper(s, visionClient)
	if services, err := gomos.Fetch(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "gomos: %v\n", err)
	} else {
		all = append(all, services...)
	}

	// Heliga Anna
	heligaAnna := scraper.NewHeligaAnnaScraper()
	if services, err := heligaAnna.Fetch(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "heligaanna: %v\n", err)
	} else {
		all = append(all, services...)
	}

	// Ryska
	ryska := scraper.NewRyskaScraper(s, visionClient)
	if services, err := ryska.Fetch(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "ryska: %v\n", err)
	} else {
		all = append(all, services...)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(all)
}
