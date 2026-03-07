package scraper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/PuerkitoBio/goquery"

	"ortodoxa-gudstjanster/internal/model"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// fetchURL fetches the content of a URL and returns the response body as bytes.
func fetchURL(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, url)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	return data, nil
}

// fetchDocument fetches a URL and parses it as an HTML document.
func fetchDocument(ctx context.Context, url string) (*goquery.Document, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, url)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}

	return doc, nil
}

// Scraper defines the interface that all church calendar scrapers must implement.
type Scraper interface {
	// Name returns the human-readable name of this scraper's source.
	Name() string

	// Fetch retrieves church services from this source.
	Fetch(ctx context.Context) ([]model.ChurchService, error)
}

// Registry holds all registered scrapers and coordinates fetching.
type Registry struct {
	scrapers []Scraper
}

// NewRegistry creates a new scraper registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a scraper to the registry.
func (r *Registry) Register(s Scraper) {
	r.scrapers = append(r.scrapers, s)
}

// Scrapers returns the list of registered scrapers.
func (r *Registry) Scrapers() []Scraper {
	return r.scrapers
}
