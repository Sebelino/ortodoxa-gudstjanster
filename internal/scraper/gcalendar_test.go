package scraper

import (
	"context"
	"testing"
	"time"
)

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

func TestParseAndExpandICS(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
DTSTART:20250316T090000Z
DTEND:20250316T103000Z
SUMMARY:Divine Liturgy
LOCATION:Sankt Ignatios Folkhögskola, Nygatan 2, Södertälje
STATUS:CONFIRMED
END:VEVENT
BEGIN:VEVENT
DTSTART:20250101T090000Z
SUMMARY:[CANCELLED] Divine Liturgy
LOCATION:Sankt Ignatios Folkhögskola, Södertälje
STATUS:CONFIRMED
END:VEVENT
BEGIN:VEVENT
DTSTART:20250301T090000Z
SUMMARY:Cancelled Service
LOCATION:Sankt Ignatios Folkhögskola, Södertälje
STATUS:CANCELLED
END:VEVENT
END:VCALENDAR`

	stockholm, _ := time.LoadLocation("Europe/Stockholm")
	events, err := ParseAndExpandICS(ics, stockholm)
	if err != nil {
		t.Fatalf("ParseAndExpandICS failed: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	// First event: normal
	if events[0].Summary != "Divine Liturgy" {
		t.Errorf("event 0 summary = %q", events[0].Summary)
	}
	if events[0].Cancelled {
		t.Error("event 0 should not be cancelled")
	}

	// Second: cancelled in summary
	if !events[1].Cancelled {
		t.Error("event 1 should be cancelled (summary contains CANCELLED)")
	}

	// Third: cancelled by STATUS
	if !events[2].Cancelled {
		t.Error("event 2 should be cancelled (STATUS=CANCELLED)")
	}
}

func TestParseAndExpandICSRecurring(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
DTSTART:20260423T180000
DTEND:20260423T190000
RRULE:FREQ=WEEKLY;COUNT=4
SUMMARY:Weekly service
STATUS:CONFIRMED
END:VEVENT
END:VCALENDAR`

	stockholm, _ := time.LoadLocation("Europe/Stockholm")
	events, err := ParseAndExpandICS(ics, stockholm)
	if err != nil {
		t.Fatalf("ParseAndExpandICS failed: %v", err)
	}

	if len(events) != 4 {
		t.Fatalf("expected 4 occurrences, got %d", len(events))
	}

	expectedDates := []string{"2026-04-23", "2026-04-30", "2026-05-07", "2026-05-14"}
	for i, ev := range events {
		got := ev.Start.Format("2006-01-02")
		if got != expectedDates[i] {
			t.Errorf("occurrence %d: got date %s, want %s", i, got, expectedDates[i])
		}
	}
}

func TestParseAndExpandICSWithExdate(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
DTSTART:20260423T180000
DTEND:20260423T190000
RRULE:FREQ=WEEKLY;COUNT=3
EXDATE:20260430T180000
SUMMARY:Weekly service
STATUS:CONFIRMED
END:VEVENT
END:VCALENDAR`

	stockholm, _ := time.LoadLocation("Europe/Stockholm")
	events, err := ParseAndExpandICS(ics, stockholm)
	if err != nil {
		t.Fatalf("ParseAndExpandICS failed: %v", err)
	}

	// 3 counted, but one excluded → 2 output events
	if len(events) != 2 {
		t.Fatalf("expected 2 occurrences (1 excluded), got %d", len(events))
	}

	if events[0].Start.Format("2006-01-02") != "2026-04-23" {
		t.Errorf("first: got %s, want 2026-04-23", events[0].Start.Format("2006-01-02"))
	}
	if events[1].Start.Format("2006-01-02") != "2026-05-07" {
		t.Errorf("second: got %s, want 2026-05-07", events[1].Start.Format("2006-01-02"))
	}
}

func TestParseAndExpandICSWithOverride(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
UID:abc123@google.com
DTSTART:20260423T180000
DTEND:20260423T190000
RRULE:FREQ=WEEKLY;COUNT=3
SUMMARY:Katekesundervisning
DESCRIPTION:Original description
STATUS:CONFIRMED
END:VEVENT
BEGIN:VEVENT
UID:abc123@google.com
DTSTART:20260423T180000
DTEND:20260423T190000
RECURRENCE-ID:20260423T180000
SUMMARY:Katekesundervisning
DESCRIPTION:Overridden for this date
STATUS:CONFIRMED
END:VEVENT
END:VCALENDAR`

	stockholm, _ := time.LoadLocation("Europe/Stockholm")
	events, err := ParseAndExpandICS(ics, stockholm)
	if err != nil {
		t.Fatalf("ParseAndExpandICS failed: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 occurrences, got %d", len(events))
	}

	// First occurrence should use the override description
	if events[0].Description != "Overridden for this date" {
		t.Errorf("first occurrence: got description %q, want %q", events[0].Description, "Overridden for this date")
	}
	// Second and third should use original
	if events[1].Description != "Original description" {
		t.Errorf("second occurrence: got description %q, want %q", events[1].Description, "Original description")
	}
}

func TestParseAndExpandICSNonRecurring(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
DTSTART:20260501T100000Z
SUMMARY:One-time event
STATUS:CONFIRMED
END:VEVENT
END:VCALENDAR`

	stockholm, _ := time.LoadLocation("Europe/Stockholm")
	events, err := ParseAndExpandICS(ics, stockholm)
	if err != nil {
		t.Fatalf("ParseAndExpandICS failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Start.Format("2006-01-02") != "2026-05-01" {
		t.Errorf("date = %s, want 2026-05-01", events[0].Start.Format("2006-01-02"))
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
