package scraper

import (
	"testing"
	"time"
)

func TestGCalendarManualParse(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
DTSTART:20260430T160000Z
DTEND:20260430T180000Z
SUMMARY:Katekesundervisning
LOCATION:Grekiska Ortodoxa Kyrkan (St.Giorgios kyrka)\, Birger Jarlsgatan 92
 \, 114 20 Stockholm\, Sweden
DESCRIPTION:Församling: St. Georgios Cathedral\nSpråk: Engelska
STATUS:CONFIRMED
END:VEVENT
END:VCALENDAR`

	events, err := parseICS(ics)
	if err != nil {
		t.Fatalf("parseICS: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	ev := events[0]
	if got := firstSubmatch(gcalManualParishRE, ev.description); got != "St. Georgios Cathedral" {
		t.Errorf("parish: got %q", got)
	}
	if got := firstSubmatch(gcalManualLanguageRE, ev.description); got != "Engelska" {
		t.Errorf("language: got %q", got)
	}
	loc, _ := time.LoadLocation("Europe/Stockholm")
	start, _, err := parseICSTimestamp(ev.dtstart, loc)
	if err != nil {
		t.Fatalf("parseICSTimestamp: %v", err)
	}
	if got := start.Format("2006-01-02 15:04"); got != "2026-04-30 18:00" {
		t.Errorf("start time: got %q", got)
	}
}

func TestExpandRecurringWeekly(t *testing.T) {
	events := []icsEvent{
		{
			summary: "Weekly service",
			dtstart: "20260423T180000",
			dtend:   "20260423T190000",
			rrule:   "FREQ=WEEKLY;COUNT=4",
		},
	}

	loc, _ := time.LoadLocation("Europe/Stockholm")
	expanded := expandRecurringEvents(events, loc)

	if len(expanded) != 4 {
		t.Fatalf("expected 4 occurrences, got %d", len(expanded))
	}

	expectedDates := []string{"20260423", "20260430", "20260507", "20260514"}
	for i, ev := range expanded {
		// dtstart is in format 20060102T150405
		dateStr := ev.dtstart[:8]
		if dateStr != expectedDates[i] {
			t.Errorf("occurrence %d: got date %s, want %s", i, dateStr, expectedDates[i])
		}
		if ev.rrule != "" {
			t.Errorf("occurrence %d: rrule should be empty after expansion", i)
		}
	}
}

func TestExpandRecurringWithExdate(t *testing.T) {
	events := []icsEvent{
		{
			summary: "Weekly service",
			dtstart: "20260423T180000",
			dtend:   "20260423T190000",
			rrule:   "FREQ=WEEKLY;COUNT=3",
			exdates: []string{"20260430T180000"},
		},
	}

	loc, _ := time.LoadLocation("Europe/Stockholm")
	expanded := expandRecurringEvents(events, loc)

	// 3 counted, but one excluded → 2 output events
	if len(expanded) != 2 {
		t.Fatalf("expected 2 occurrences (1 excluded), got %d", len(expanded))
	}

	if expanded[0].dtstart[:8] != "20260423" {
		t.Errorf("first: got %s, want 20260423", expanded[0].dtstart[:8])
	}
	if expanded[1].dtstart[:8] != "20260507" {
		t.Errorf("second: got %s, want 20260507", expanded[1].dtstart[:8])
	}
}

func TestExpandNonRecurring(t *testing.T) {
	events := []icsEvent{
		{
			summary: "One-time event",
			dtstart: "20260501T100000Z",
		},
	}

	loc, _ := time.LoadLocation("Europe/Stockholm")
	expanded := expandRecurringEvents(events, loc)

	if len(expanded) != 1 {
		t.Fatalf("expected 1 event, got %d", len(expanded))
	}
	if expanded[0].dtstart != "20260501T100000Z" {
		t.Errorf("should be unchanged, got %s", expanded[0].dtstart)
	}
}
