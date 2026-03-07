package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ortodoxa-gudstjanster/internal/model"
)

// mockFetcher implements ServiceFetcher for testing.
type mockFetcher struct {
	services []model.ChurchService
	batchID  string
	err      error
}

func (m *mockFetcher) GetAllServices(ctx context.Context) ([]model.ChurchService, error) {
	return m.services, m.err
}

func (m *mockFetcher) GetLatestBatchID(ctx context.Context) (string, error) {
	return m.batchID, m.err
}

func ptr(s string) *string { return &s }

// --- parseStartTime ---

func TestParseStartTime(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"18:00", "180000"},
		{"9:30", "093000"},
		{"18:00 - 20:00", "180000"},
		{"18:00 – 20:00", "180000"},
		{"1800", "180000"},
		{"1800 - ca 2000", "180000"},
		{"08:30", "083000"},
		{"", ""},
		{"TBD", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseStartTime(tt.input)
			if got != tt.want {
				t.Errorf("parseStartTime(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- escapeICS ---

func TestEscapeICS(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple text", "simple text"},
		{"semi;colon", "semi\\;colon"},
		{"com,ma", "com\\,ma"},
		{"new\nline", "new\\nline"},
		{"back\\slash", "back\\\\slash"},
		{"all;of,them\nhere\\now", "all\\;of\\,them\\nhere\\\\now"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := escapeICS(tt.input)
			if got != tt.want {
				t.Errorf("escapeICS(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- langCategory ---

func TestLangCategory(t *testing.T) {
	tests := []struct {
		name   string
		svc    model.ChurchService
		want   string
	}{
		{
			name: "explicit Svenska",
			svc:  model.ChurchService{EventLanguage: ptr("Svenska")},
			want: "Svenska",
		},
		{
			name: "explicit Engelska",
			svc:  model.ChurchService{EventLanguage: ptr("Engelska")},
			want: "Engelska",
		},
		{
			name: "parish language svenska fallback",
			svc:  model.ChurchService{ParishLanguage: ptr("Svenska, finska")},
			want: "Svenska",
		},
		{
			name: "parish language engelska fallback",
			svc:  model.ChurchService{ParishLanguage: ptr("Engelska")},
			want: "Engelska",
		},
		{
			name: "no language info",
			svc:  model.ChurchService{},
			want: "Övrigt",
		},
		{
			name: "event language overrides parish",
			svc:  model.ChurchService{EventLanguage: ptr("Engelska"), ParishLanguage: ptr("Svenska")},
			want: "Engelska",
		},
		{
			name: "unknown event language falls through",
			svc:  model.ChurchService{EventLanguage: ptr("Finska")},
			want: "Övrigt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := langCategory(tt.svc)
			if got != tt.want {
				t.Errorf("langCategory() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- getClientIP ---

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name       string
		xff        string
		xri        string
		remoteAddr string
		want       string
	}{
		{
			name:       "X-Forwarded-For single IP",
			xff:        "1.2.3.4",
			remoteAddr: "5.6.7.8:1234",
			want:       "1.2.3.4",
		},
		{
			name:       "X-Forwarded-For multiple IPs uses last",
			xff:        "1.2.3.4, 10.0.0.1, 9.8.7.6",
			remoteAddr: "5.6.7.8:1234",
			want:       "9.8.7.6",
		},
		{
			name:       "X-Real-IP fallback",
			xri:        "1.2.3.4",
			remoteAddr: "5.6.7.8:1234",
			want:       "1.2.3.4",
		},
		{
			name:       "RemoteAddr fallback",
			remoteAddr: "5.6.7.8:1234",
			want:       "5.6.7.8",
		},
		{
			name:       "RemoteAddr without port",
			remoteAddr: "5.6.7.8",
			want:       "5.6.7.8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.xri != "" {
				r.Header.Set("X-Real-IP", tt.xri)
			}
			r.RemoteAddr = tt.remoteAddr

			got := getClientIP(r)
			if got != tt.want {
				t.Errorf("getClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- rateLimiter ---

func TestRateLimiter(t *testing.T) {
	rl := newRateLimiter(2, time.Hour)

	if !rl.allow("1.2.3.4") {
		t.Error("first request should be allowed")
	}
	if !rl.allow("1.2.3.4") {
		t.Error("second request should be allowed")
	}
	if rl.allow("1.2.3.4") {
		t.Error("third request should be denied")
	}
	// Different IP should still be allowed
	if !rl.allow("5.6.7.8") {
		t.Error("different IP should be allowed")
	}
}

func TestRateLimiterWindowExpiry(t *testing.T) {
	rl := newRateLimiter(1, 10*time.Millisecond)

	if !rl.allow("1.2.3.4") {
		t.Error("first request should be allowed")
	}
	if rl.allow("1.2.3.4") {
		t.Error("second request should be denied")
	}

	time.Sleep(15 * time.Millisecond)

	if !rl.allow("1.2.3.4") {
		t.Error("request after window expiry should be allowed")
	}
}

// --- filterAndSort ---

func TestFilterAndSort(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	longAgo := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")

	services := []model.ChurchService{
		{Date: tomorrow, ServiceName: "B", Time: ptr("10:00")},
		{Date: today, ServiceName: "A", Time: ptr("09:00")},
		{Date: longAgo, ServiceName: "Old"},
		{Date: yesterday, ServiceName: "C", Time: ptr("18:00")},
	}

	result := filterAndSort(services)

	// longAgo should be filtered out (older than 7 days)
	for _, s := range result {
		if s.Date == longAgo {
			t.Error("service from 30 days ago should be filtered out")
		}
	}

	// Should be sorted by date, then time
	if len(result) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(result))
	}
	for i := 1; i < len(result); i++ {
		prev := result[i-1]
		curr := result[i]
		if prev.Date > curr.Date {
			t.Errorf("not sorted by date: %s > %s", prev.Date, curr.Date)
		}
	}
}

// --- generateICS ---

func TestGenerateICS(t *testing.T) {
	loc := "Stockholm"
	timeStr := "10:00"
	services := []model.ChurchService{
		{
			Parish:      "Test Parish",
			Source:      "Test Parish",
			Date:        "2026-03-08",
			DayOfWeek:   "Söndag",
			ServiceName: "Helig Liturgi",
			Location:    &loc,
			Time:        &timeStr,
		},
	}

	ics := generateICS(services)

	checks := []string{
		"BEGIN:VCALENDAR",
		"END:VCALENDAR",
		"BEGIN:VEVENT",
		"END:VEVENT",
		"SUMMARY:Helig Liturgi",
		"LOCATION:Stockholm",
		"DTSTART;TZID=Europe/Stockholm:20260308T100000",
		"DURATION:PT1H",
		"Församling: Test Parish",
		"VERSION:2.0",
	}

	for _, check := range checks {
		if !strings.Contains(ics, check) {
			t.Errorf("ICS output missing %q", check)
		}
	}
}

func TestGenerateICSAllDayEvent(t *testing.T) {
	services := []model.ChurchService{
		{
			Parish:      "Test",
			Source:      "Test",
			Date:        "2026-03-08",
			ServiceName: "Fastedag",
		},
	}

	ics := generateICS(services)

	if !strings.Contains(ics, "DTSTART;VALUE=DATE:20260308") {
		t.Error("all-day event should use VALUE=DATE format")
	}
}

func TestGenerateICSSummarySimplification(t *testing.T) {
	tests := []struct {
		serviceName string
		title       string
		wantSummary string
	}{
		{"Morgongudstjänst", "", "Gudstjänst"},
		{"Aftongudstjänst", "", "Gudstjänst"},
		{"Kvällsgudstjänst", "", "Gudstjänst"},
		{"Helig Liturgi", "", "Helig Liturgi"},
		{"Morgongudstjänst", "Morgon", "Morgon"},
		{"Helig Liturgi", "Liturgi", "Liturgi"},
	}

	for _, tt := range tests {
		t.Run(tt.serviceName+"/"+tt.title, func(t *testing.T) {
			services := []model.ChurchService{
				{
					Parish:      "Test",
					Source:      "Test",
					Date:        "2026-03-08",
					ServiceName: tt.serviceName,
					Title:       tt.title,
				},
			}
			ics := generateICS(services)
			expected := "SUMMARY:" + escapeICS(tt.wantSummary)
			if !strings.Contains(ics, expected) {
				t.Errorf("expected SUMMARY to contain %q, got ICS:\n%s", expected, ics)
			}
		})
	}
}

// --- HTTP handler tests ---

func TestHandleHealth(t *testing.T) {
	h := New(&mockFetcher{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/health", nil)

	h.handleHealth(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() != "ok" {
		t.Errorf("body = %q, want %q", w.Body.String(), "ok")
	}
}

func TestHandleServices(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	fetcher := &mockFetcher{
		services: []model.ChurchService{
			{
				Parish:      "Test",
				Source:      "Test",
				Date:        today,
				DayOfWeek:   "Söndag",
				ServiceName: "Liturgi",
			},
		},
	}

	h := New(fetcher)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/services", nil)

	h.handleServices(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var result []model.ChurchService
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("got %d services, want 1", len(result))
	}
}

func TestHandleICSExcludeFilter(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	fetcher := &mockFetcher{
		services: []model.ChurchService{
			{Parish: "KeepMe", Source: "KeepMe", Date: today, ServiceName: "A"},
			{Parish: "DropMe", Source: "DropMe", Date: today, ServiceName: "B"},
		},
	}

	h := New(fetcher)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/calendar.ics?exclude=DropMe", nil)

	h.handleICS(w, r)

	body := w.Body.String()
	if !strings.Contains(body, "KeepMe") {
		t.Error("ICS should contain KeepMe parish")
	}
	if strings.Contains(body, "CATEGORIES:DropMe") {
		t.Error("ICS should not contain DropMe parish")
	}
}

func TestHandleICSExcludeLangFilter(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	fetcher := &mockFetcher{
		services: []model.ChurchService{
			{Parish: "A", Source: "A", Date: today, ServiceName: "S1", EventLanguage: ptr("Svenska")},
			{Parish: "B", Source: "B", Date: today, ServiceName: "S2", EventLanguage: ptr("Engelska")},
		},
	}

	h := New(fetcher)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/calendar.ics?excludeLang=Engelska", nil)

	h.handleICS(w, r)

	body := w.Body.String()
	if !strings.Contains(body, "CATEGORIES:A") {
		t.Error("ICS should contain parish A (Svenska)")
	}
	if strings.Contains(body, "CATEGORIES:B") {
		t.Error("ICS should not contain parish B (Engelska)")
	}
}

func TestHandleIndexNotFound(t *testing.T) {
	h := New(&mockFetcher{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/nonexistent", nil)

	h.handleIndex(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleFeedbackGet(t *testing.T) {
	h := New(&mockFetcher{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/feedback", nil)

	h.handleFeedback(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
		t.Errorf("Content-Type = %q, want text/html", w.Header().Get("Content-Type"))
	}
}

func TestHandleFeedbackPostHoneypot(t *testing.T) {
	h := New(&mockFetcher{})
	w := httptest.NewRecorder()
	body := `{"type":"error","message":"test","website":"http://spam.com"}`
	r := httptest.NewRequest("POST", "/feedback", strings.NewReader(body))

	h.handleFeedback(w, r)

	// Should silently accept (200) but not actually send
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleFeedbackPostMissingFields(t *testing.T) {
	h := New(&mockFetcher{})
	h.SetSMTP(nil) // no SMTP
	w := httptest.NewRecorder()
	body := `{"type":"","message":"","timestamp":1000}`
	r := httptest.NewRequest("POST", "/feedback", strings.NewReader(body))
	r.RemoteAddr = "1.2.3.4:5678"

	h.handleFeedback(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleFeedbackMethodNotAllowed(t *testing.T) {
	h := New(&mockFetcher{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("PUT", "/feedback", nil)

	h.handleFeedback(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleLastUpdated(t *testing.T) {
	fetcher := &mockFetcher{batchID: "batch-123"}
	h := New(fetcher)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/last-updated", nil)

	h.handleLastUpdated(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if result["batch_id"] != "batch-123" {
		t.Errorf("batch_id = %q, want %q", result["batch_id"], "batch-123")
	}
}
