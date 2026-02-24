package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"church-services/internal/model"
	"church-services/internal/scraper"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
	gomos := scraper.NewGomosScraper()
	if services, err := gomos.Fetch(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "gomos: %v\n", err)
	} else {
		all = append(all, services...)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(all)
}
