package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
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
	mux.HandleFunc("/calendar.ics", h.handleICS)
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

func (h *Handler) handleICS(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()

	services := h.fetchAllWithCache(ctx)
	services = filterAndSort(services)

	// Parse excluded sources from query parameter
	excludeParam := r.URL.Query().Get("exclude")
	if excludeParam != "" {
		excluded := make(map[string]bool)
		for _, source := range strings.Split(excludeParam, ",") {
			excluded[strings.TrimSpace(source)] = true
		}
		var filtered []model.ChurchService
		for _, s := range services {
			if !excluded[s.Source] {
				filtered = append(filtered, s)
			}
		}
		services = filtered
	}

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline; filename=\"ortodoxa-gudstjanster.ics\"")

	// Generate ICS content
	ics := generateICS(services)
	w.Write([]byte(ics))
}

func generateICS(services []model.ChurchService) string {
	var sb strings.Builder

	sb.WriteString("BEGIN:VCALENDAR\r\n")
	sb.WriteString("VERSION:2.0\r\n")
	sb.WriteString("PRODID:-//Ortodoxa Gudstjänster//SV\r\n")
	sb.WriteString("CALSCALE:GREGORIAN\r\n")
	sb.WriteString("METHOD:PUBLISH\r\n")
	sb.WriteString("X-WR-CALNAME:Ortodoxa Gudstjänster\r\n")

	for i, s := range services {
		sb.WriteString("BEGIN:VEVENT\r\n")

		// Generate unique ID
		uid := fmt.Sprintf("%s-%d@ortodoxa-gudstjanster", s.Date, i)
		sb.WriteString(fmt.Sprintf("UID:%s\r\n", uid))

		// Date and time
		if s.Time != nil && *s.Time != "" {
			if startTime := parseStartTime(*s.Time); startTime != "" {
				dtstart := strings.ReplaceAll(s.Date, "-", "") + "T" + startTime
				sb.WriteString(fmt.Sprintf("DTSTART:%s\r\n", dtstart))
				// Assume 1.5 hour duration for services
				sb.WriteString(fmt.Sprintf("DURATION:PT1H30M\r\n"))
			}
		} else {
			// All-day event
			dtstart := strings.ReplaceAll(s.Date, "-", "")
			sb.WriteString(fmt.Sprintf("DTSTART;VALUE=DATE:%s\r\n", dtstart))
		}

		// Summary (service name)
		summary := escapeICS(s.ServiceName)
		sb.WriteString(fmt.Sprintf("SUMMARY:%s\r\n", summary))

		// Location
		if s.Location != nil && *s.Location != "" {
			location := escapeICS(*s.Location)
			sb.WriteString(fmt.Sprintf("LOCATION:%s\r\n", location))
		}

		// Description with additional details
		var desc []string
		desc = append(desc, fmt.Sprintf("Församling: %s", s.Source))
		if s.Language != nil && *s.Language != "" {
			desc = append(desc, fmt.Sprintf("Språk: %s", *s.Language))
		}
		if s.Occasion != nil && *s.Occasion != "" {
			desc = append(desc, fmt.Sprintf("Tillfälle: %s", *s.Occasion))
		}
		if s.Notes != nil && *s.Notes != "" {
			desc = append(desc, fmt.Sprintf("Info: %s", *s.Notes))
		}
		if s.SourceURL != "" {
			desc = append(desc, fmt.Sprintf("Källa: %s", s.SourceURL))
		}
		description := escapeICS(strings.Join(desc, "\n"))
		sb.WriteString(fmt.Sprintf("DESCRIPTION:%s\r\n", description))

		// Categories
		sb.WriteString(fmt.Sprintf("CATEGORIES:%s\r\n", escapeICS(s.Source)))

		// Timestamp
		now := time.Now().UTC().Format("20060102T150405Z")
		sb.WriteString(fmt.Sprintf("DTSTAMP:%s\r\n", now))

		sb.WriteString("END:VEVENT\r\n")
	}

	sb.WriteString("END:VCALENDAR\r\n")
	return sb.String()
}

func escapeICS(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ";", "\\;")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

// parseStartTime extracts the start time from a time string and returns it in HHMMSS format.
// Handles formats like "18:00", "1800", "18:00 - 20:00", "1800 - ca 2000", etc.
func parseStartTime(timeStr string) string {
	// Remove any range part (everything after " - " or " – ")
	timeStr = strings.Split(timeStr, " - ")[0]
	timeStr = strings.Split(timeStr, " – ")[0]
	timeStr = strings.TrimSpace(timeStr)

	// Try to parse HH:MM format
	if parts := strings.Split(timeStr, ":"); len(parts) >= 2 {
		hour := strings.TrimSpace(parts[0])
		minute := strings.TrimSpace(parts[1])
		// Take only first 2 chars of minute in case there's extra stuff
		if len(minute) > 2 {
			minute = minute[:2]
		}
		if len(hour) <= 2 && len(minute) == 2 {
			return fmt.Sprintf("%02s%s00", hour, minute)
		}
	}

	// Try to parse HHMM format (4 digits)
	if len(timeStr) >= 4 {
		// Check if first 4 chars are digits
		candidate := timeStr[:4]
		isDigits := true
		for _, c := range candidate {
			if c < '0' || c > '9' {
				isDigits = false
				break
			}
		}
		if isDigits {
			return candidate + "00"
		}
	}

	return ""
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
