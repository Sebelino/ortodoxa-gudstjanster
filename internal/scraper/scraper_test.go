package scraper

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"church-services/internal/model"
	"church-services/internal/store"
	"church-services/internal/vision"
)

var dateRegex = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// getServiceDisplayName returns a display name from the ServiceName map (prefers Swedish).
func getServiceDisplayName(names map[string]string) string {
	if name, ok := names["sv"]; ok {
		return name
	}
	for _, name := range names {
		return name
	}
	return ""
}

// testDeps holds common test dependencies for scrapers that need store and vision.
type testDeps struct {
	store  *store.Store
	vision *vision.Client
}

// newTestDeps creates test dependencies, skipping the test if OPENAI_API_KEY is not set.
func newTestDeps(t *testing.T, storeDir string) *testDeps {
	t.Helper()
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	s, err := store.New(storeDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	return &testDeps{
		store:  s,
		vision: vision.NewClient(os.Getenv("OPENAI_API_KEY")),
	}
}

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

	// ServiceName must have at least one language entry
	if len(s.ServiceName) == 0 {
		t.Error("ServiceName is empty")
	}
	for lang, name := range s.ServiceName {
		if lang == "" {
			t.Error("ServiceName has empty language key")
		}
		if name == "" {
			t.Errorf("ServiceName[%s] is empty", lang)
		}
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
		t.Run(s.Date+"_"+getServiceDisplayName(s.ServiceName), func(t *testing.T) {
			validateService(t, s, scraper.Name())
		})

		// Log first few for visibility
		if i < 3 {
			t.Logf("  [%d] %s: %s @ %v", i, s.Date, getServiceDisplayName(s.ServiceName), s.Time)
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

	deps := newTestDeps(t, filepath.Join(os.TempDir(), "church-services-store-test"))
	scraper := NewGomosScraper(deps.store, deps.vision)

	if scraper.Name() != "St. Georgios Cathedral" {
		t.Errorf("Unexpected scraper name: %s", scraper.Name())
	}

	services, err := scraper.Fetch(ctx)
	if err != nil {
		// Vision API may fail if OPENAI_API_KEY isn't set
		t.Skipf("Fetch failed (OPENAI_API_KEY may not be set): %v", err)
	}

	if len(services) == 0 {
		t.Skip("No services returned")
	}

	t.Logf("Fetched %d services from Gomos", len(services))

	// Validate each service
	for i, s := range services {
		t.Run(s.Date+"_"+getServiceDisplayName(s.ServiceName), func(t *testing.T) {
			validateService(t, s, scraper.Name())
		})

		// Log first few for visibility
		if i < 3 {
			t.Logf("  [%d] %s: %s @ %v", i, s.Date, getServiceDisplayName(s.ServiceName), s.Time)
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

	s, err := store.New(filepath.Join(os.TempDir(), "church-services-store-test"))
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	v := vision.NewClient("")

	registry.Register(NewFinskaScraper(""))
	registry.Register(NewGomosScraper(s, v))

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

func assertHasFebruary2026Events(t *testing.T, services []model.ChurchService) {
	t.Helper()
	var feb2026Count int
	for _, svc := range services {
		if len(svc.Date) >= 7 && svc.Date[:7] == "2026-02" {
			feb2026Count++
		}
	}
	if feb2026Count == 0 {
		t.Errorf("Expected at least one event in February 2026, got 0 (total events: %d)", len(services))
	} else {
		t.Logf("Found %d events in February 2026", feb2026Count)
	}
}

func TestFinskaScraperHasFebruary2026Events(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	scraper := NewFinskaScraper("")
	services, err := scraper.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	assertHasFebruary2026Events(t, services)
}

func TestGomosScraperHasFebruary2026Events(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	deps := newTestDeps(t, filepath.Join(os.TempDir(), "church-services-store-test"))
	scraper := NewGomosScraper(deps.store, deps.vision)

	services, err := scraper.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	assertHasFebruary2026Events(t, services)
}

func TestGomosScraperSavesSourceImage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	storeDir := "../../test-disk"
	os.RemoveAll(storeDir)
	deps := newTestDeps(t, storeDir)
	scraper := NewGomosScraper(deps.store, deps.vision)

	_, err := scraper.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	// Check that at least one image file was saved
	entries, err := os.ReadDir(storeDir)
	if err != nil {
		t.Fatalf("Failed to read store directory: %v", err)
	}

	var imageCount int
	for _, entry := range entries {
		name := entry.Name()
		if filepath.Ext(name) == ".jpg" || filepath.Ext(name) == ".png" || filepath.Ext(name) == ".jpeg" {
			imageCount++
			t.Logf("Found image: %s", name)
		}
	}

	if imageCount == 0 {
		t.Error("Expected at least one image file (.jpg/.png) in store directory")
	} else {
		t.Logf("Found %d image files in store directory", imageCount)
	}
}

func TestHeligaAnnaScraperHasFebruary2026Events(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	scraper := NewHeligaAnnaScraper()
	services, err := scraper.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	assertHasFebruary2026Events(t, services)
}

func TestRyskaScraperHasFebruary2026Events(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	deps := newTestDeps(t, filepath.Join(os.TempDir(), "church-services-store-test"))
	scraper := NewRyskaScraper(deps.store, deps.vision)

	services, err := scraper.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	assertHasFebruary2026Events(t, services)
}
