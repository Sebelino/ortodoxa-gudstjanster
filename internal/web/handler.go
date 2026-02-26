package web

import (
	"context"
	"embed"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"

	"church-services/internal/cache"
	"church-services/internal/model"
	"church-services/internal/scraper"
)

//go:embed templates/index.html
var templates embed.FS

// Handler holds the HTTP handlers and their dependencies.
type Handler struct {
	registry *scraper.Registry
	cache    *cache.Cache
}

// New creates a new Handler with the given scraper registry and cache.
func New(registry *scraper.Registry, c *cache.Cache) *Handler {
	return &Handler{
		registry: registry,
		cache:    c,
	}
}

// RegisterRoutes registers all HTTP routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", h.noCache(h.handleIndex))
	mux.HandleFunc("/services", h.noCache(h.handleServices))
	mux.HandleFunc("/health", h.handleHealth)
}

func (h *Handler) noCache(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		next(w, r)
	}
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, _ := templates.ReadFile("templates/index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (h *Handler) handleServices(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()

	services := h.fetchAllWithCache(ctx)
	services = filterAndSort(services)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(services)
}

func filterAndSort(services []model.ChurchService) []model.ChurchService {
	today := time.Now().Format("2006-01-02")

	// Filter out past events
	var future []model.ChurchService
	for _, s := range services {
		if s.Date >= today {
			future = append(future, s)
		}
	}

	// Sort by date (and time if available)
	sort.Slice(future, func(i, j int) bool {
		if future[i].Date != future[j].Date {
			return future[i].Date < future[j].Date
		}
		// Same date - sort by time if available
		timeI := ""
		timeJ := ""
		if future[i].Time != nil {
			timeI = *future[i].Time
		}
		if future[j].Time != nil {
			timeJ = *future[j].Time
		}
		return timeI < timeJ
	})

	return future
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (h *Handler) fetchAllWithCache(ctx context.Context) []model.ChurchService {
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		services []model.ChurchService
	)

	for _, s := range h.registry.Scrapers() {
		wg.Add(1)
		go func(scraper scraper.Scraper) {
			defer wg.Done()

			// Check cache first
			if cached, ok := h.cache.Get(scraper.Name()); ok {
				mu.Lock()
				services = append(services, cached...)
				mu.Unlock()
				return
			}

			// Fetch fresh data
			result, err := scraper.Fetch(ctx)
			if err != nil {
				return
			}

			// Store in cache
			h.cache.Set(scraper.Name(), result)

			mu.Lock()
			services = append(services, result...)
			mu.Unlock()
		}(s)
	}

	wg.Wait()
	return services
}
