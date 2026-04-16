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
