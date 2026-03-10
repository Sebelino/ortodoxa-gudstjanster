package scraper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"ortodoxa-gudstjanster/internal/vision"
)

func TestFetchPageText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><head><title>Test</title><style>body{}</style></head>
			<body><h1>Ortodoxt Sommarlager</h1><p>Måndag 13/7 till Torsdag 16/7 2026</p>
			<script>console.log("hi")</script></body></html>`))
	}))
	defer srv.Close()

	text, err := fetchPageText(t.Context(), srv.URL)
	if err != nil {
		t.Fatalf("fetchPageText failed: %v", err)
	}

	if text == "" {
		t.Fatal("fetchPageText returned empty text")
	}

	// Should contain visible text but not script/style content
	if !contains(text, "Sommarlager") {
		t.Errorf("expected text to contain 'Sommarlager', got: %s", text)
	}
	if contains(text, "console.log") {
		t.Error("text should not contain script content")
	}
	if contains(text, "body{}") {
		t.Error("text should not contain style content")
	}
}

func TestSommarlagerEventsToServices(t *testing.T) {
	s := &SommarlagerScraper{}
	events := []vision.CampEvent{
		{Date: "2026-07-13", EndDate: "2026-07-16", DayOfWeek: "Måndag", ServiceName: "Ortodoxt sommarläger", Notes: "Sjöbonäs lägergård, Kinnarumma"},
		{Date: "2026-06-10", DayOfWeek: "Onsdag", ServiceName: "Sista anmälningsdag: Ortodoxt sommarläger"},
	}

	services := s.eventsToServices(events)
	if len(services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(services))
	}

	// Camp event — multi-day
	if services[0].Date != "2026-07-13" {
		t.Errorf("date = %q, want 2026-07-13", services[0].Date)
	}
	if services[0].Parish != "" {
		t.Errorf("parish = %q, want empty", services[0].Parish)
	}
	if services[0].SourceURL != sommarlagerURL {
		t.Errorf("source_url = %q, want %q", services[0].SourceURL, sommarlagerURL)
	}
	if services[0].StartTime == nil {
		t.Fatal("camp event StartTime should be set")
	}
	if services[0].EndTime == nil {
		t.Fatal("camp event EndTime should be set")
	}
	if services[0].StartTime.Day() != 13 || services[0].StartTime.Hour() != 0 {
		t.Errorf("StartTime = %v, want July 13 00:00", services[0].StartTime)
	}
	if services[0].EndTime.Day() != 16 || services[0].EndTime.Hour() != 23 || services[0].EndTime.Minute() != 59 {
		t.Errorf("EndTime = %v, want July 16 23:59", services[0].EndTime)
	}

	// Deadline event — single day, no start/end time
	if services[1].StartTime != nil {
		t.Errorf("deadline StartTime should be nil, got %v", services[1].StartTime)
	}
	if services[1].EndTime != nil {
		t.Errorf("deadline EndTime should be nil, got %v", services[1].EndTime)
	}
}

func TestFindRegistrationLink(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "finds Anmälan link",
			html: `<body><a href="/?page_id=60">Anmälan</a></body>`,
			want: sommarlagerURL + "/?page_id=60",
		},
		{
			name: "finds anmälning link",
			html: `<body><a href="/register">Anmälning här</a></body>`,
			want: sommarlagerURL + "/register",
		},
		{
			name: "finds registrera link",
			html: `<body><a href="https://example.com/reg">Registrera dig</a></body>`,
			want: "https://example.com/reg",
		},
		{
			name: "ignores non-registration links",
			html: `<body><a href="/about">Om oss</a><a href="/contact">Kontakt</a></body>`,
			want: "",
		},
		{
			name: "no links at all",
			html: `<body><p>No links here</p></body>`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			if err != nil {
				t.Fatalf("parse html: %v", err)
			}
			got := findRegistrationLink(doc)
			if got != tt.want {
				t.Errorf("findRegistrationLink() = %q, want %q", got, tt.want)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
