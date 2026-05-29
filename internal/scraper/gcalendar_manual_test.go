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
LOCATION:Grekiska Ortodoxa Kyrkan (St.Giorgios kyrka)\, Birger Jarlsgatan 92\, 114 20 Stockholm\, Sweden
DESCRIPTION:Församling: St. Georgios Cathedral\nSpråk: Engelska
STATUS:CONFIRMED
END:VEVENT
END:VCALENDAR`

	loc, _ := time.LoadLocation("Europe/Stockholm")
	events, err := ParseAndExpandICS(ics, loc)
	if err != nil {
		t.Fatalf("ParseAndExpandICS: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	ev := events[0]
	if got := firstSubmatch(gcalManualParishRE, ev.Description); got != "St. Georgios Cathedral" {
		t.Errorf("parish: got %q", got)
	}
	if got := firstSubmatch(gcalManualLanguageRE, ev.Description); got != "Engelska" {
		t.Errorf("language: got %q", got)
	}
	if got := ev.Start.Format("2006-01-02 15:04"); got != "2026-04-30 18:00" {
		t.Errorf("start time: got %q", got)
	}
}
