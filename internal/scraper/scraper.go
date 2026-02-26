package scraper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/PuerkitoBio/goquery"

	"church-services/internal/model"
)

// fetchURL fetches the content of a URL and returns the response body as bytes.
func fetchURL(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching URL: %w", err)
	}
	defer resp.Body.Close()

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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching URL: %w", err)
	}
	defer resp.Body.Close()

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

// FetchAll runs all scrapers concurrently and returns combined results.
// Errors from individual scrapers are logged but don't prevent other scrapers from running.
func (r *Registry) FetchAll(ctx context.Context) []model.ChurchService {
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		services []model.ChurchService
	)

	for _, s := range r.scrapers {
		wg.Add(1)
		go func(scraper Scraper) {
			defer wg.Done()

			result, err := scraper.Fetch(ctx)
			if err != nil {
				// Log but don't fail - partial results are acceptable
				return
			}

			mu.Lock()
			services = append(services, result...)
			mu.Unlock()
		}(s)
	}

	wg.Wait()
	return services
}

// Scrapers returns the list of registered scrapers.
func (r *Registry) Scrapers() []Scraper {
	return r.scrapers
}
