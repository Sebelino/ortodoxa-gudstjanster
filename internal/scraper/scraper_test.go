package scraper

import (
	"context"
	"regexp"
	"testing"
	"time"

	"church-services/internal/model"
)

var dateRegex = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func validateService(t *testing.T, s model.ChurchService, scraperName string) {
	t.Helper()

	// Source must match scraper name
	if s.Source == "" {
		t.Error("Source is empty")
	}
	if s.Source != scraperName {
		t.Errorf("Source mismatch: got %q, want %q", s.Source, scraperName)
	}

	// Date must be valid YYYY-MM-DD format
	if !dateRegex.MatchString(s.Date) {
		t.Errorf("Invalid date format: %q", s.Date)
	}

	// Date must be parseable
	parsed, err := time.Parse("2006-01-02", s.Date)
	if err != nil {
		t.Errorf("Date not parseable: %q: %v", s.Date, err)
	}

	// Date should be reasonable (not in the distant past or future)
	now := time.Now()
	oneYearAgo := now.AddDate(-1, 0, 0)
	twoYearsFromNow := now.AddDate(2, 0, 0)
	if parsed.Before(oneYearAgo) || parsed.After(twoYearsFromNow) {
		t.Errorf("Date outside reasonable range: %s", s.Date)
	}

	// DayOfWeek must not be empty
	if s.DayOfWeek == "" {
		t.Error("DayOfWeek is empty")
	}

	// ServiceName must not be empty
	if s.ServiceName == "" {
		t.Error("ServiceName is empty")
	}

	// If Time is set, it should look like a time
	if s.Time != nil && *s.Time != "" {
		timeRegex := regexp.MustCompile(`^\d{1,2}:\d{2}`)
		if !timeRegex.MatchString(*s.Time) {
			t.Errorf("Time doesn't look like a time: %q", *s.Time)
		}
	}
}

func TestFinskaScraper(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	scraper := NewFinskaScraper("")

	if scraper.Name() != "Finska Ortodoxa Församlingen" {
		t.Errorf("Unexpected scraper name: %s", scraper.Name())
	}

	services, err := scraper.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if len(services) == 0 {
		t.Fatal("No services returned")
	}

	t.Logf("Fetched %d services from Finska", len(services))

	// Validate each service
	for i, s := range services {
		t.Run(s.Date+"_"+s.ServiceName, func(t *testing.T) {
			validateService(t, s, scraper.Name())
		})

		// Log first few for visibility
		if i < 3 {
			t.Logf("  [%d] %s: %s @ %v", i, s.Date, s.ServiceName, s.Time)
		}
	}

	// Check that we have some variety in dates
	dates := make(map[string]bool)
	for _, s := range services {
		dates[s.Date] = true
	}
	if len(dates) < 2 {
		t.Errorf("Expected multiple dates, got %d unique dates", len(dates))
	}
}

func TestGomosScraper(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	scraper := NewGomosScraper()

	if scraper.Name() != "St. Georgios Cathedral" {
		t.Errorf("Unexpected scraper name: %s", scraper.Name())
	}

	services, err := scraper.Fetch(ctx)
	if err != nil {
		// OCR-based scraper may fail if tesseract isn't installed
		t.Skipf("Fetch failed (tesseract may not be installed): %v", err)
	}

	if len(services) == 0 {
		t.Skip("No services returned (OCR may have failed to parse)")
	}

	t.Logf("Fetched %d services from Gomos", len(services))

	// Validate each service
	for i, s := range services {
		t.Run(s.Date+"_"+s.ServiceName, func(t *testing.T) {
			validateService(t, s, scraper.Name())
		})

		// Log first few for visibility
		if i < 3 {
			t.Logf("  [%d] %s: %s @ %v", i, s.Date, s.ServiceName, s.Time)
		}
	}

	// All Gomos services should have the cathedral location
	for _, s := range services {
		if s.Location == nil || *s.Location == "" {
			t.Error("Gomos service missing location")
		}
	}
}

func TestRegistry(t *testing.T) {
	registry := NewRegistry()

	if len(registry.Scrapers()) != 0 {
		t.Error("New registry should be empty")
	}

	registry.Register(NewFinskaScraper(""))
	registry.Register(NewGomosScraper())

	if len(registry.Scrapers()) != 2 {
		t.Errorf("Expected 2 scrapers, got %d", len(registry.Scrapers()))
	}
}

func TestRegistryFetchAll(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	registry := NewRegistry()
	registry.Register(NewFinskaScraper(""))

	services := registry.FetchAll(ctx)

	if len(services) == 0 {
		t.Error("FetchAll returned no services")
	}

	t.Logf("FetchAll returned %d services", len(services))
}
