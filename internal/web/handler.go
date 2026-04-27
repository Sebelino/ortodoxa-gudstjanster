package web

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"ortodoxa-gudstjanster/internal/email"
	"ortodoxa-gudstjanster/internal/model"
)

//go:embed templates/*
var templates embed.FS

// ServiceFetcher is an interface for fetching church services.
type ServiceFetcher interface {
	GetAllServices(ctx context.Context) ([]model.ChurchService, error)
	GetServiceByID(ctx context.Context, id string) (*model.ChurchService, error)
	GetLatestBatchID(ctx context.Context) (string, error)
}

// rateLimiter tracks submissions per IP address.
type rateLimiter struct {
	mu        sync.Mutex
	requests  map[string][]time.Time
	limit     int
	window    time.Duration
	lastPrune time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Periodically prune expired IPs to prevent unbounded map growth
	if now.Sub(rl.lastPrune) > rl.window {
		for k, times := range rl.requests {
			var recent []time.Time
			for _, t := range times {
				if t.After(cutoff) {
					recent = append(recent, t)
				}
			}
			if len(recent) == 0 {
				delete(rl.requests, k)
			} else {
				rl.requests[k] = recent
			}
		}
		rl.lastPrune = now
	}

	// Filter out old requests for this IP
	var recent []time.Time
	for _, t := range rl.requests[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}

	if len(recent) >= rl.limit {
		rl.requests[ip] = recent
		return false
	}

	rl.requests[ip] = append(recent, now)
	return true
}

// Handler holds the HTTP handlers and their dependencies.
type Handler struct {
	fetcher     ServiceFetcher
	smtp        *email.SMTPConfig
	rateLimiter *rateLimiter
}

// New creates a new Handler with the given service fetcher.
func New(fetcher ServiceFetcher) *Handler {
	return &Handler{
		fetcher:     fetcher,
		rateLimiter: newRateLimiter(3, time.Hour), // 3 submissions per hour per IP
	}
}

// SetSMTP configures SMTP for sending feedback emails.
func (h *Handler) SetSMTP(config *email.SMTPConfig) {
	h.smtp = config
}

// RegisterRoutes registers all HTTP routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", h.noCache(h.handleIndex))
	mux.HandleFunc("/services", h.noCache(h.handleServices))
	mux.HandleFunc("/calendar.ics", h.noCache(h.handleICS))
	mux.HandleFunc("/api/parishes", h.handleParishesAPI)
	mux.HandleFunc("/parishes", h.handleParishesPage)
	mux.HandleFunc("/parish/", h.handleParish)
	mux.HandleFunc("/event/", h.handleEvent)
	mux.HandleFunc("/feedback", h.handleFeedback)
	mux.HandleFunc("/last-updated", h.noCache(h.handleLastUpdated))
	mux.HandleFunc("/health", h.handleHealth)
	mux.HandleFunc("/favicon.svg", h.handleFavicon)
	mux.HandleFunc("/favicon-48.png", h.handleFavicon48)
	mux.HandleFunc("/icon-192.png", h.handleIcon192)
	mux.HandleFunc("/icon-512.png", h.handleIcon512)
	mux.HandleFunc("/apple-touch-icon.png", h.handleAppleTouchIcon)
	mux.HandleFunc("/manifest.json", h.handleManifest)
	mux.HandleFunc("/sw.js", h.handleServiceWorker)
	mux.HandleFunc("/about", h.handleAbout)
	mux.HandleFunc("/privacy", h.handlePrivacy)
	mux.HandleFunc("/robots.txt", h.handleRobots)
	mux.HandleFunc("/sitemap.xml", h.handleSitemap)
	mux.HandleFunc("/.well-known/assetlinks.json", h.handleAssetLinks)
}

func (h *Handler) noCache(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		next(w, r)
	}
}

// parseWithTheme parses a named template file together with the shared _theme.html
// partial, making the theme-css, theme-flash, and theme-js blocks available.
func parseWithTheme(name string) (*template.Template, error) {
	return template.ParseFS(templates, "templates/"+name, "templates/_theme.html")
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	tmpl, err := parseWithTheme("index.html")
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Inject Event JSON-LD for SEO (search engines don't execute JS)
	var jsonLD template.HTML
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if services, err := h.fetcher.GetAllServices(ctx); err == nil {
		services = filterAndSort(services)
		if jld := buildEventJSONLD(services); jld != "" {
			jsonLD = template.HTML(jld)
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.Execute(w, struct{ JSONLD template.HTML }{JSONLD: jsonLD})
}

func buildEventJSONLD(services []model.ChurchService) string {
	const maxEvents = 50
	n := len(services)
	if n == 0 {
		return ""
	}
	if n > maxEvents {
		n = maxEvents
	}

	var events []map[string]interface{}
	for _, s := range services[:n] {
		event := map[string]interface{}{
			"@type":       "Event",
			"name":        s.ServiceName,
			"eventStatus": "https://schema.org/EventScheduled",
		}

		if s.Title != "" {
			event["description"] = s.ServiceName
		} else if s.Notes != nil && *s.Notes != "" {
			event["description"] = *s.Notes
		}

		if s.StartTime != nil {
			event["startDate"] = s.StartTime.Format(time.RFC3339)
			if s.EndTime != nil {
				event["endDate"] = s.EndTime.Format(time.RFC3339)
			}
		} else if s.Date != "" {
			event["startDate"] = s.Date
		}

		if s.Location != nil && *s.Location != "" {
			event["location"] = map[string]interface{}{
				"@type": "Place",
				"name":  *s.Location,
				"address": map[string]interface{}{
					"@type":           "PostalAddress",
					"streetAddress":   *s.Location,
					"addressLocality": "Stockholm",
					"addressCountry":  "SE",
				},
			}
		}

		if s.Parish != "" {
			organizer := map[string]interface{}{
				"@type": "Organization",
				"name":  s.Parish,
			}
			if s.SourceURL != "" {
				organizer["url"] = s.SourceURL
			}
			event["organizer"] = organizer
		}

		events = append(events, event)
	}

	wrapper := map[string]interface{}{
		"@context": "https://schema.org",
		"@graph":   events,
	}

	jsonBytes, err := json.Marshal(wrapper)
	if err != nil {
		return ""
	}

	return `    <script type="application/ld+json">` + string(jsonBytes) + `</script>`
}

func (h *Handler) handleServices(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	services, err := h.fetcher.GetAllServices(ctx)
	if err != nil {
		http.Error(w, "Failed to fetch services", http.StatusInternalServerError)
		return
	}
	services = filterAndSort(services)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(services)
}

func (h *Handler) handleICS(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	services, err := h.fetcher.GetAllServices(ctx)
	if err != nil {
		http.Error(w, "Failed to fetch services", http.StatusInternalServerError)
		return
	}
	services = filterAndSort(services)

	// Parish filter priority (highest to lowest):
	//   1. includeCounties= and/or includeParishes= (new style, generated by current UI)
	//   2. include= (legacy parish whitelist, kept for old ICS links)
	//   3. exclude= (oldest legacy blacklist, kept for oldest ICS links) — scoped to Stockholm
	//   4. no params — default to Stockholm only
	queryValues := r.URL.Query()
	_, hasIncludeCounties := queryValues["includeCounties"]
	_, hasIncludeParishes := queryValues["includeParishes"]
	if hasIncludeCounties || hasIncludeParishes {
		included := make(map[string]bool)
		if countiesParam := queryValues.Get("includeCounties"); countiesParam != "" {
			for _, county := range strings.Split(countiesParam, ",") {
				county = strings.TrimSpace(county)
				for _, p := range parishes {
					if p.County == county {
						included[p.Name] = true
					}
				}
			}
		}
		if parishesParam := queryValues.Get("includeParishes"); parishesParam != "" {
			for _, name := range strings.Split(parishesParam, ",") {
				if n := strings.TrimSpace(name); n != "" {
					included[n] = true
				}
			}
		}
		var filtered []model.ChurchService
		for _, s := range services {
			if included[parishGroup(s)] {
				filtered = append(filtered, s)
			}
		}
		services = filtered
	} else if includeParam := queryValues.Get("include"); includeParam != "" {
		// Legacy: include= parish whitelist
		included := make(map[string]bool)
		for _, source := range strings.Split(includeParam, ",") {
			if s := strings.TrimSpace(source); s != "" {
				included[s] = true
			}
		}
		var filtered []model.ChurchService
		for _, s := range services {
			if included[parishGroup(s)] {
				filtered = append(filtered, s)
			}
		}
		services = filtered
	} else {
		stockholmParishes := make(map[string]bool)
		for _, p := range parishes {
			if p.County == "Stockholm" {
				stockholmParishes[p.Name] = true
			}
		}
		if excludeParam := queryValues.Get("exclude"); excludeParam != "" {
			// Oldest legacy: exclude= blacklist, scoped to Stockholm
			excluded := make(map[string]bool)
			for _, source := range strings.Split(excludeParam, ",") {
				excluded[strings.TrimSpace(source)] = true
			}
			var filtered []model.ChurchService
			for _, s := range services {
				if stockholmParishes[parishGroup(s)] && !excluded[parishGroup(s)] {
					filtered = append(filtered, s)
				}
			}
			services = filtered
		} else {
			// No params: default to Stockholm only
			var filtered []model.ChurchService
			for _, s := range services {
				if stockholmParishes[parishGroup(s)] {
					filtered = append(filtered, s)
				}
			}
			services = filtered
		}
	}

	// Language filter: includeLang= (whitelist) takes precedence over excludeLang= (blacklist, legacy)
	if includeLangParam := r.URL.Query().Get("includeLang"); includeLangParam != "" {
		includedLangs := make(map[string]bool)
		for _, lang := range strings.Split(includeLangParam, ",") {
			if l := strings.TrimSpace(lang); l != "" {
				includedLangs[l] = true
			}
		}
		var filtered []model.ChurchService
		for _, s := range services {
			if includedLangs[langCategory(s)] {
				filtered = append(filtered, s)
			}
		}
		services = filtered
	} else if excludeLangParam := r.URL.Query().Get("excludeLang"); excludeLangParam != "" {
		excludedLangs := make(map[string]bool)
		for _, lang := range strings.Split(excludeLangParam, ",") {
			excludedLangs[strings.TrimSpace(lang)] = true
		}
		var filtered []model.ChurchService
		for _, s := range services {
			if !excludedLangs[langCategory(s)] {
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
	sb.WriteString("X-WR-TIMEZONE:Europe/Stockholm\r\n")

	for _, s := range services {
		sb.WriteString("BEGIN:VEVENT\r\n")

		// Generate stable UID from service fields
		timeStr := ""
		if s.Time != nil {
			timeStr = *s.Time
		}
		uidData := fmt.Sprintf("%s|%s|%s|%s", s.Source, s.Date, s.ServiceName, timeStr)
		uidHash := sha256.Sum256([]byte(uidData))
		uid := hex.EncodeToString(uidHash[:16]) + "@ortodoxa-gudstjanster"
		sb.WriteString(fmt.Sprintf("UID:%s\r\n", uid))

		// Date and time
		if s.StartTime != nil {
			dtstart := s.StartTime.Format("20060102T150405")
			sb.WriteString(fmt.Sprintf("DTSTART;TZID=Europe/Stockholm:%s\r\n", dtstart))
			if s.EndTime != nil {
				dtend := s.EndTime.Format("20060102T150405")
				sb.WriteString(fmt.Sprintf("DTEND;TZID=Europe/Stockholm:%s\r\n", dtend))
			} else {
				sb.WriteString("DURATION:PT1H\r\n")
			}
		} else if s.Time != nil && *s.Time != "" {
			if startTime := parseStartTime(*s.Time); startTime != "" {
				dtstart := strings.ReplaceAll(s.Date, "-", "") + "T" + startTime
				sb.WriteString(fmt.Sprintf("DTSTART;TZID=Europe/Stockholm:%s\r\n", dtstart))
				sb.WriteString("DURATION:PT1H\r\n")
			}
		} else {
			// All-day event
			dtstart := strings.ReplaceAll(s.Date, "-", "")
			sb.WriteString(fmt.Sprintf("DTSTART;VALUE=DATE:%s\r\n", dtstart))
		}

		// Summary (use short title if available, else full service name)
		summaryText := s.ServiceName
		if s.Title != "" {
			summaryText = s.Title
		}
		if summaryText == "Morgongudstjänst" || summaryText == "Aftongudstjänst" || summaryText == "Kvällsgudstjänst" {
			summaryText = "Gudstjänst"
		}
		summary := escapeICS(summaryText)
		sb.WriteString(fmt.Sprintf("SUMMARY:%s\r\n", summary))

		// Location
		if s.Location != nil && *s.Location != "" {
			location := escapeICS(*s.Location)
			sb.WriteString(fmt.Sprintf("LOCATION:%s\r\n", location))
		}

		// Description with additional details
		var desc []string
		desc = append(desc, fmt.Sprintf("Församling: %s", parishGroup(s)))
		desc = append(desc, fmt.Sprintf("Beskrivning: %s", s.ServiceName))
		if s.EventLanguage != nil && *s.EventLanguage != "" {
			desc = append(desc, fmt.Sprintf("Språk: %s", *s.EventLanguage))
		} else if s.ParishLanguage != nil && *s.ParishLanguage != "" {
			desc = append(desc, fmt.Sprintf("Språk: %s (ej angivet)", *s.ParishLanguage))
		}
		if s.Occasion != nil && *s.Occasion != "" {
			desc = append(desc, fmt.Sprintf("Tillfälle: %s", *s.Occasion))
		}
		if s.Notes != nil && *s.Notes != "" {
			desc = append(desc, fmt.Sprintf("Info: %s", *s.Notes))
		}
		if s.SourceURL != "" {
			desc = append(desc, fmt.Sprintf("Källa: %s", s.SourceURL))
		} else if s.Source != "" {
			desc = append(desc, fmt.Sprintf("Källa: %s", s.Source))
		}
		description := escapeICS(strings.Join(desc, "\n"))
		sb.WriteString(fmt.Sprintf("DESCRIPTION:%s\r\n", description))

		// Categories
		sb.WriteString(fmt.Sprintf("CATEGORIES:%s\r\n", escapeICS(parishGroup(s))))

		// Timestamp
		now := time.Now().UTC().Format("20060102T150405Z")
		sb.WriteString(fmt.Sprintf("DTSTAMP:%s\r\n", now))

		sb.WriteString("END:VEVENT\r\n")
	}

	sb.WriteString("END:VCALENDAR\r\n")
	return sb.String()
}

// parishGroup returns the parish name, or "Övrigt" for services without a parish.
func parishGroup(s model.ChurchService) string {
	if s.Parish == "" {
		return "Övrigt"
	}
	return s.Parish
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
			h := 0
			m := 0
			fmt.Sscanf(hour, "%d", &h)
			fmt.Sscanf(minute, "%d", &m)
			if h >= 0 && h <= 23 && m >= 0 && m <= 59 {
				return fmt.Sprintf("%02d%02d00", h, m)
			}
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
			h := 0
			m := 0
			fmt.Sscanf(candidate[:2], "%d", &h)
			fmt.Sscanf(candidate[2:], "%d", &m)
			if h >= 0 && h <= 23 && m >= 0 && m <= 59 {
				return candidate + "00"
			}
		}
	}

	return ""
}

func langCategory(s model.ChurchService) string {
	el := ""
	if s.EventLanguage != nil {
		el = *s.EventLanguage
	}
	pl := ""
	if s.ParishLanguage != nil {
		pl = strings.ToLower(*s.ParishLanguage)
	}
	if el == "Svenska" {
		return "Svenska"
	}
	if el == "Engelska" {
		return "Engelska"
	}
	if el == "" {
		if strings.Contains(pl, "svenska") {
			return "Svenska"
		}
		if strings.Contains(pl, "engelska") {
			return "Engelska"
		}
	}
	return "Övrigt"
}

func filterAndSort(services []model.ChurchService) []model.ChurchService {
	cutoff := time.Now().AddDate(0, 0, -7).Format("2006-01-02")

	var future []model.ChurchService
	for _, s := range services {
		if s.Date >= cutoff {
			future = append(future, s)
		}
	}

	future = deduplicateServices(future)

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

// deduplicateServices removes duplicate events that share the same parish,
// date, and start time (first component of the time range). When duplicates
// are found, the event with the most detail is kept.
func deduplicateServices(services []model.ChurchService) []model.ChurchService {
	type dedupeKey struct {
		parish string
		date   string
		time   string
	}

	best := make(map[dedupeKey]int) // key → index in result
	var result []model.ChurchService

	for _, s := range services {
		t := ""
		if s.Time != nil {
			// Use only the start time for deduplication (before " - ")
			t = *s.Time
			if idx := strings.Index(t, " - "); idx >= 0 {
				t = t[:idx]
			}
		}
		key := dedupeKey{parish: parishGroup(s), date: s.Date, time: t}

		if existingIdx, ok := best[key]; ok {
			// Keep the one with more detail
			if serviceDetail(s) > serviceDetail(result[existingIdx]) {
				result[existingIdx] = s
			}
		} else {
			best[key] = len(result)
			result = append(result, s)
		}
	}

	return result
}

// serviceDetail returns a score representing how much detail a service has.
// Higher is more detailed.
func serviceDetail(s model.ChurchService) int {
	score := 0
	if s.Occasion != nil && *s.Occasion != "" {
		score++
	}
	if s.Notes != nil && *s.Notes != "" {
		score++
	}
	if s.Location != nil && *s.Location != "" {
		score++
	}
	if s.Time != nil && *s.Time != "" {
		score++
	}
	if s.Title != "" {
		score++
	}
	if s.EventLanguage != nil {
		score++
	}
	return score
}

func (h *Handler) handleLastUpdated(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	batchID, err := h.fetcher.GetLatestBatchID(ctx)
	if err != nil {
		http.Error(w, "Failed to fetch last updated", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{"batch_id": batchID})
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (h *Handler) handleFavicon(w http.ResponseWriter, r *http.Request) {
	data, err := templates.ReadFile("templates/favicon.svg")
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

func (h *Handler) handleFavicon48(w http.ResponseWriter, r *http.Request) {
	serveIcon(w, 48)
}

func (h *Handler) handleIcon192(w http.ResponseWriter, r *http.Request) {
	serveIcon(w, 192)
}

func (h *Handler) handleIcon512(w http.ResponseWriter, r *http.Request) {
	serveIcon(w, 512)
}

func (h *Handler) handleAppleTouchIcon(w http.ResponseWriter, r *http.Request) {
	serveIcon(w, 180)
}

func (h *Handler) handleManifest(w http.ResponseWriter, r *http.Request) {
	data, err := templates.ReadFile("templates/manifest.json")
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

func (h *Handler) handleServiceWorker(w http.ResponseWriter, r *http.Request) {
	data, err := templates.ReadFile("templates/sw.js")
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}

func (h *Handler) handleAssetLinks(w http.ResponseWriter, r *http.Request) {
	data, err := templates.ReadFile("templates/assetlinks.json")
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

func (h *Handler) handleRobots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "User-agent: *\nAllow: /\nSitemap: https://ortodoxagudstjanster.se/sitemap.xml\n")
}

func (h *Handler) handleSitemap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url>
    <loc>https://ortodoxagudstjanster.se/</loc>
    <priority>1.0</priority>
  </url>
  <url>
    <loc>https://ortodoxagudstjanster.se/parishes</loc>
    <priority>0.8</priority>
  </url>`)
	for _, p := range parishes {
		fmt.Fprintf(&sb, `
  <url>
    <loc>https://ortodoxagudstjanster.se/parish/%s</loc>
    <priority>0.7</priority>
  </url>`, p.Slug)
	}
	sb.WriteString(`
  <url>
    <loc>https://ortodoxagudstjanster.se/feedback</loc>
    <priority>0.3</priority>
  </url>
</urlset>`)
	w.Write([]byte(sb.String()))
}

func (h *Handler) handleParishesAPI(w http.ResponseWriter, r *http.Request) {
	type parishJSON struct {
		Slug      string   `json:"slug"`
		Name      string   `json:"name"`
		ShortName string   `json:"short_name"`
		Address   string   `json:"address"`
		City      string   `json:"city"`
		County    string   `json:"county"`
		Website   string   `json:"website"`
		Languages []string `json:"languages"`
		Tradition string   `json:"tradition"`
	}
	result := make([]parishJSON, len(parishes))
	for i, p := range parishes {
		result[i] = parishJSON{
			Slug:      p.Slug,
			Name:      p.Name,
			ShortName: p.ShortName,
			Address:   p.Address,
			City:      p.City,
			County:    p.County,
			Website:   p.Website,
			Languages: p.Languages,
			Tradition: p.Tradition,
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	json.NewEncoder(w).Encode(result)
}

func (h *Handler) handleParishesPage(w http.ResponseWriter, r *http.Request) {
	tmpl, err := parseWithTheme("parishes.html")
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.Execute(w, parishes)
}

func (h *Handler) handleParish(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimPrefix(r.URL.Path, "/parish/")
	p, ok := parishBySlug[slug]
	if !ok {
		http.NotFound(w, r)
		return
	}

	tmpl, err := parseWithTheme("parish.html")
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	websiteDisplay := strings.TrimPrefix(p.Website, "https://")
	websiteDisplay = strings.TrimPrefix(websiteDisplay, "www.")

	data := struct {
		ParishInfo
		LanguagesStr   string
		WebsiteDisplay string
	}{
		ParishInfo:     p,
		LanguagesStr:   strings.Join(p.Languages, ", "),
		WebsiteDisplay: websiteDisplay,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.Execute(w, data)
}

func (h *Handler) handleEvent(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/event/")
	if id == "" {
		http.NotFound(w, r)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	svc, err := h.fetcher.GetServiceByID(ctx, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	tmpl, err := parseWithTheme("event.html")
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Look up parish slug
	var parishSlug string
	for _, p := range parishes {
		if p.Name == svc.Parish {
			parishSlug = p.Slug
			break
		}
	}

	// Build language display string
	var language string
	if svc.EventLanguage != nil {
		language = *svc.EventLanguage
	} else if svc.ParishLanguage != nil {
		language = *svc.ParishLanguage + " (ej angivet)"
	}

	// Determine timezone abbreviation from the event date
	tz := "CET"
	if svc.StartTime != nil {
		_, offset := svc.StartTime.Zone()
		if offset == 7200 {
			tz = "CEST"
		}
	} else if d, err := time.Parse("2006-01-02", svc.Date); err == nil {
		stockholm, _ := time.LoadLocation("Europe/Stockholm")
		_, offset := time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, stockholm).Zone()
		if offset == 7200 {
			tz = "CEST"
		}
	}

	data := struct {
		ServiceName string
		Title       string
		Date        string
		DayOfWeek   string
		Time        string
		Timezone    string
		Parish      string
		ParishSlug  string
		Location    string
		Language    string
		Occasion    string
		Notes       string
		Source      string
		SourceURL   string
	}{
		ServiceName: svc.ServiceName,
		Title:       svc.Title,
		Date:        svc.Date,
		DayOfWeek:   svc.DayOfWeek,
		Timezone:    tz,
		Parish:      svc.Parish,
		ParishSlug:  parishSlug,
		Source:      svc.Source,
		SourceURL:   svc.SourceURL,
		Language:    language,
	}
	if svc.Time != nil {
		data.Time = *svc.Time
	}
	if svc.Location != nil {
		data.Location = *svc.Location
	}
	if svc.Occasion != nil {
		data.Occasion = *svc.Occasion
	}
	if svc.Notes != nil {
		data.Notes = *svc.Notes
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.Execute(w, data)
}

func (h *Handler) handleFeedback(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		tmpl, err := parseWithTheme("feedback.html")
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, nil)
		return
	}

	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 64*1024) // 64 KB limit

		var feedback struct {
			Type      string `json:"type"`
			Email     string `json:"email"`
			Message   string `json:"message"`
			Website   string `json:"website"`   // Honeypot field
			Timestamp int64  `json:"timestamp"` // Form load timestamp
		}

		if err := json.NewDecoder(r.Body).Decode(&feedback); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		// Honeypot check - bots will fill this hidden field
		if feedback.Website != "" {
			// Silently accept but don't send email
			w.WriteHeader(http.StatusOK)
			return
		}

		// Time-based check - form must be open for at least 3 seconds
		if feedback.Timestamp > 0 {
			elapsed := time.Now().UnixMilli() - feedback.Timestamp
			if elapsed < 3000 {
				// Too fast, likely a bot
				w.WriteHeader(http.StatusOK)
				return
			}
		}

		// Rate limiting check
		clientIP := getClientIP(r)
		if !h.rateLimiter.allow(clientIP) {
			http.Error(w, "För många förfrågningar. Försök igen senare.", http.StatusTooManyRequests)
			return
		}

		if feedback.Type == "" || feedback.Message == "" {
			http.Error(w, "Type and message are required", http.StatusBadRequest)
			return
		}

		// Send email notification
		if err := h.sendFeedbackEmail(feedback.Type, feedback.Email, feedback.Message); err != nil {
			log.Printf("Failed to send feedback email: %v", err)
			http.Error(w, "Failed to send feedback", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (h *Handler) handleAbout(w http.ResponseWriter, r *http.Request) {
	tmpl, err := parseWithTheme("about.html")
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.Execute(w, nil)
}

func (h *Handler) handlePrivacy(w http.ResponseWriter, r *http.Request) {
	tmpl, err := parseWithTheme("privacy.html")
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.Execute(w, nil)
}

func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (set by proxies/load balancers)
	// Use the last IP — on Cloud Run the proxy appends the real client IP
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[len(parts)-1])
	}
	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Fall back to RemoteAddr
	if idx := strings.LastIndex(r.RemoteAddr, ":"); idx != -1 {
		return r.RemoteAddr[:idx]
	}
	return r.RemoteAddr
}

func (h *Handler) sendFeedbackEmail(feedbackType, senderEmail, message string) error {
	if h.smtp == nil {
		return fmt.Errorf("SMTP not configured")
	}

	typeLabels := map[string]string{
		"error":      "Fel i schemat",
		"new_parish": "Lägg till församling",
		"suggestion": "Förslag",
		"other":      "Annat",
	}

	typeLabel := typeLabels[feedbackType]
	if typeLabel == "" {
		typeLabel = feedbackType
	}

	replyTo := senderEmail
	if replyTo == "" {
		replyTo = "ingen e-post angiven"
	}

	subject := fmt.Sprintf("Feedback: %s", typeLabel)
	body := fmt.Sprintf("Typ: %s\r\nFrån: %s\r\n\r\nMeddelande:\r\n%s", typeLabel, replyTo, email.NormalizeNewlines(message))

	return h.smtp.Send(subject, body)
}

