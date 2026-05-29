package scraper

import (
	"context"
	"testing"
	"time"
)

func TestRomanianScraper(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	scraper := NewRomanianScraper()

	if scraper.Name() != romanianSourceName {
		t.Errorf("Name() = %q, want %q", scraper.Name(), romanianSourceName)
	}

	services, err := scraper.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if len(services) == 0 {
		t.Fatal("No services returned")
	}

	t.Logf("Fetched %d services", len(services))

	// Fetch upstream ICS to compare locations
	data, err := fetchURL(ctx, romanianICSURL)
	if err != nil {
		t.Fatalf("fetching upstream ICS: %v", err)
	}

	stockholm, _ := time.LoadLocation("Europe/Stockholm")
	upstreamEvents, err := ParseAndExpandICS(string(data), stockholm)
	if err != nil {
		t.Fatalf("parsing upstream ICS: %v", err)
	}

	// Build map of date+summary → location from upstream
	type eventKey struct{ date, summary string }
	upstream := make(map[eventKey]string)
	for _, ev := range upstreamEvents {
		if ev.Cancelled || ev.Location == "" {
			continue
		}
		upstream[eventKey{ev.Start.Format("2006-01-02"), ev.Summary}] = ev.Location
	}

	// Verify scraped services preserve upstream locations
	for _, svc := range services {
		t.Run(svc.Date+"_"+svc.ServiceName, func(t *testing.T) {
			validateService(t, svc, romanianCalendarName)

			if svc.Location == nil || *svc.Location == "" {
				t.Error("Location is empty")
				return
			}

			key := eventKey{svc.Date, svc.ServiceName}
			if icsLoc, ok := upstream[key]; ok {
				if *svc.Location != icsLoc {
					t.Errorf("Location mismatch for %s on %s:\n  got:  %q\n  want: %q", svc.ServiceName, svc.Date, *svc.Location, icsLoc)
				}
			}
		})
	}
}

func TestRomanianScraperUsesUpstreamLocation(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
DTSTART:20250529T100000Z
DTEND:20250529T110000Z
SUMMARY:Holy Unction
LOCATION:Karolinska University Hospital
STATUS:CONFIRMED
END:VEVENT
BEGIN:VEVENT
DTSTART:20250601T080000Z
DTEND:20250601T100000Z
SUMMARY:Divine Liturgy
STATUS:CONFIRMED
END:VEVENT
END:VCALENDAR`

	stockholm, _ := time.LoadLocation("Europe/Stockholm")
	events, err := ParseAndExpandICS(ics, stockholm)
	if err != nil {
		t.Fatalf("ParseAndExpandICS failed: %v", err)
	}

	for _, ev := range events {
		location := romanianLocation
		if ev.Location != "" {
			location = ev.Location
		}

		switch ev.Summary {
		case "Holy Unction":
			if location != "Karolinska University Hospital" {
				t.Errorf("Expected upstream location, got %q", location)
			}
		case "Divine Liturgy":
			if location != romanianLocation {
				t.Errorf("Expected default location for event without location, got %q", location)
			}
		}
	}
}
