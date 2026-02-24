package scraper

import (
	"context"
	"sync"

	"church-services/internal/model"
)

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
