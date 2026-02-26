package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"sort"
	"strings"
	"sync"
	"time"

	"ortodoxa-gudstjanster/internal/cache"
	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/scraper"
)

//go:embed templates/*.html
var templates embed.FS

// SMTPConfig holds SMTP configuration for sending emails.
type SMTPConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	To       string
}

// rateLimiter tracks submissions per IP address.
type rateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
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

	// Filter out old requests
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
	registry    *scraper.Registry
	cache       *cache.Cache
	smtp        *SMTPConfig
	rateLimiter *rateLimiter
}

// New creates a new Handler with the given scraper registry and cache.
func New(registry *scraper.Registry, c *cache.Cache) *Handler {
	return &Handler{
		registry:    registry,
		cache:       c,
		rateLimiter: newRateLimiter(3, time.Hour), // 3 submissions per hour per IP
	}
}

// SetSMTP configures SMTP for sending feedback emails.
func (h *Handler) SetSMTP(config *SMTPConfig) {
	h.smtp = config
}

// RegisterRoutes registers all HTTP routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", h.noCache(h.handleIndex))
	mux.HandleFunc("/services", h.noCache(h.handleServices))
	mux.HandleFunc("/calendar.ics", h.handleICS)
	mux.HandleFunc("/feedback", h.handleFeedback)
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

func (h *Handler) handleFeedback(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		data, _ := templates.ReadFile("templates/feedback.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
		return
	}

	if r.Method == http.MethodPost {
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
			http.Error(w, "Failed to send feedback", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (set by proxies/load balancers)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the list
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
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

func (h *Handler) sendFeedbackEmail(feedbackType, email, message string) error {
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

	replyTo := email
	if replyTo == "" {
		replyTo = "ingen e-post angiven"
	}

	subject := fmt.Sprintf("Feedback: %s", typeLabel)
	body := fmt.Sprintf("Typ: %s\nFrån: %s\n\nMeddelande:\n%s", typeLabel, replyTo, message)

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		h.smtp.User, h.smtp.To, subject, body)

	auth := smtp.PlainAuth("", h.smtp.User, h.smtp.Password, h.smtp.Host)
	addr := h.smtp.Host + ":" + h.smtp.Port

	return smtp.SendMail(addr, auth, h.smtp.User, []string{h.smtp.To}, []byte(msg))
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
