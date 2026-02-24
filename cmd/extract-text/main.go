package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"church-services/internal/scraper"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	text, err := scraper.ExtractRyskaScheduleText(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(text)
}
