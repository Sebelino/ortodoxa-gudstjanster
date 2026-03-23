package scraper

import (
	"context"
	"testing"
	"time"
)

func TestParseICS(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
DTSTART:20250316T090000Z
DTEND:20250316T103000Z
SUMMARY:Second Sunday of Great Lent - Divine Liturgy
LOCATION:Sankt Ignatios Folkhögskola and Sankt Ignatios College\, Nygatan 2
 \, 151 72 Södertälje\, Sweden
DESCRIPTION:Fr Milutin
STATUS:CONFIRMED
END:VEVENT
BEGIN:VEVENT
DTSTART:20250419T190000Z
DTEND:20250419T200000Z
SUMMARY:Reading of the Book of Acts
LOCATION:Petruskyrkan\, Kyrkvägen 27\, 182 74 Stocksund\, Sweden
STATUS:CONFIRMED
END:VEVENT
BEGIN:VEVENT
DTSTART:20250101T090000Z
SUMMARY:[CANCELLED] Divine Liturgy
LOCATION:Sankt Ignatios Folkhögskola and Sankt Ignatios College\, Nygatan 2\, Södertälje
STATUS:CONFIRMED
END:VEVENT
BEGIN:VEVENT
DTSTART:20250201T090000Z
SUMMARY:Divine Liturgy
LOCATION:Ryska Ortodoxa Kyrkan i Stockholm\, Birger Jarlsgatan 98
STATUS:CONFIRMED
END:VEVENT
BEGIN:VEVENT
DTSTART:20250301T090000Z
SUMMARY:Cancelled Service
LOCATION:Sankt Ignatios Folkhögskola\, Södertälje
STATUS:CANCELLED
END:VEVENT
END:VCALENDAR`

	events, err := parseICS(ics)
	if err != nil {
		t.Fatalf("parseICS failed: %v", err)
	}

	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(events))
	}

	// First event: St. Ignatios
	if events[0].summary != "Second Sunday of Great Lent - Divine Liturgy" {
		t.Errorf("event 0 summary = %q", events[0].summary)
	}
	if events[0].description != "Fr Milutin" {
		t.Errorf("event 0 description = %q", events[0].description)
	}
	if events[0].cancelled {
		t.Error("event 0 should not be cancelled")
	}

	// Second event: Heliga Anna (Petruskyrkan)
	if events[1].summary != "Reading of the Book of Acts" {
		t.Errorf("event 1 summary = %q", events[1].summary)
	}

	// Third: cancelled in summary
	if !events[2].cancelled {
		t.Error("event 2 should be cancelled (summary contains CANCELLED)")
	}

	// Fourth: Ryska location (should be skipped by matchLocation)
	parish, _, _ := matchLocation(events[3].location)
	if parish != "" {
		t.Errorf("event 3 should not match any parish, got %q", parish)
	}

	// Fifth: cancelled by STATUS
	if !events[4].cancelled {
		t.Error("event 4 should be cancelled (STATUS=CANCELLED)")
	}
}

func TestMatchLocation(t *testing.T) {
	tests := []struct {
		location   string
		wantParish string
	}{
		{"Petruskyrkan, Kyrkvägen 27, 182 74 Stocksund, Sweden", "Heliga Anna av Novgorod"},
		{"Heliga Annas Ortodoxa kyrkoförsamling, Kyrkvägen 27, Stocksund", "Heliga Anna av Novgorod"},
		{"Sankt Ignatios Folkhögskola and Sankt Ignatios College, Nygatan 2, 151 72 Södertälje, Sweden", "St. Ignatios"},
		{"Sankt Ignatios andliga akademi, Nygatan 2, 151 72 Södertälje, Sweden", "St. Ignatios"},
		{"Ryska Ortodoxa Kyrkan i Stockholm, Birger Jarlsgatan 98", ""},
		{"Zoom", ""},
		{"", ""},
	}

	for _, tt := range tests {
		parish, _, _ := matchLocation(tt.location)
		if parish != tt.wantParish {
			t.Errorf("matchLocation(%q) parish = %q, want %q", tt.location, parish, tt.wantParish)
		}
	}
}

func TestParseICSTimestamp(t *testing.T) {
	stockholm, _ := time.LoadLocation("Europe/Stockholm")

	tests := []struct {
		ts       string
		wantDate string
		wantTime string
		allDay   bool
	}{
		{"20250316T090000Z", "2025-03-16", "10:00", false}, // UTC+1 in March
		{"20250616T090000Z", "2025-06-16", "11:00", false}, // UTC+2 in June (DST)
		{"20250101", "2025-01-01", "", true},
		{"20250316T100000", "2025-03-16", "10:00", false},
	}

	for _, tt := range tests {
		parsed, allDay, err := parseICSTimestamp(tt.ts, stockholm)
		if err != nil {
			t.Errorf("parseICSTimestamp(%q) error: %v", tt.ts, err)
			continue
		}
		if allDay != tt.allDay {
			t.Errorf("parseICSTimestamp(%q) allDay = %v, want %v", tt.ts, allDay, tt.allDay)
		}
		if parsed.Format("2006-01-02") != tt.wantDate {
			t.Errorf("parseICSTimestamp(%q) date = %s, want %s", tt.ts, parsed.Format("2006-01-02"), tt.wantDate)
		}
		if !allDay && parsed.Format("15:04") != tt.wantTime {
			t.Errorf("parseICSTimestamp(%q) time = %s, want %s", tt.ts, parsed.Format("15:04"), tt.wantTime)
		}
	}
}

func TestGCalendarScraper(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	scraper := NewGCalendarScraper()

	if scraper.Name() != gcalendarSourceName {
		t.Errorf("Name() = %q, want %q", scraper.Name(), gcalendarSourceName)
	}

	services, err := scraper.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if len(services) == 0 {
		t.Fatal("No services returned")
	}

	t.Logf("Fetched %d services", len(services))

	// Check that we have both parishes
	parishes := make(map[string]int)
	for _, s := range services {
		parishes[s.Parish]++
	}
	t.Logf("Parishes: %v", parishes)

	if parishes[parishHeligaAnna] == 0 {
		t.Error("No Heliga Anna events found")
	}
	if parishes[parishStIgnatios] == 0 {
		t.Error("No St. Ignatios events found")
	}

	// Validate each service
	for _, s := range services {
		t.Run(s.Date+"_"+s.ServiceName, func(t *testing.T) {
			validateService(t, s, scraper.Name())
		})
	}
}
