package scraper

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/srpska"
)

const (
	gcalendarSourceName = "Google Calendar (Heliga Anna / St. Ignatios)"
	gcalendarURL        = "https://calendar.google.com/calendar/ical/59cfef47ecdd52608c31b98333b9e00a2ffa46cb49582aea9632981de7d70642@group.calendar.google.com/public/basic.ics"
	gcalendarSourcePage = "https://calendar.google.com/calendar/u/0/embed?src=59cfef47ecdd52608c31b98333b9e00a2ffa46cb49582aea9632981de7d70642@group.calendar.google.com&ctz=Europe/Stockholm"

	parishHeligaAnna = "Heliga Anna av Novgorod"
	parishStIgnatios = "St. Ignatios"
)

// locationMapping maps a substring in the LOCATION field to a parish and display location.
var locationMapping = []struct {
	substring      string
	parish         string
	location       string
	parishLanguage string
}{
	{"Petruskyrkan", parishHeligaAnna, "Heliga Anna, Kyrkvägen 27, 182 74 Stocksund, Sweden", "Svenska"},
	{"Heliga Annas Ortodoxa", parishHeligaAnna, "Heliga Anna, Kyrkvägen 27, 182 74 Stocksund, Sweden", "Svenska"},
	{"Sankt Ignatios Folkhögskola", parishStIgnatios, "Sankt Ignatios Folkhögskola, Nygatan 2, 151 72 Södertälje", "Svenska, grekiska, serbiska"},
	{"Sankt Ignatios andliga akademi", parishStIgnatios, "Sankt Ignatios andliga akademi, Nygatan 2, 151 72 Södertälje", "Svenska, grekiska, serbiska"},
	// Catch-all for Södertälje addresses not matching above
	{"Södertälje", parishStIgnatios, "Nygatan 2, 151 72 Södertälje", "Svenska, grekiska, serbiska"},
}

// GCalendarScraper fetches events from a public Google Calendar ICS feed.
type GCalendarScraper struct{ NoteCollector }

func NewGCalendarScraper() *GCalendarScraper {
	return &GCalendarScraper{}
}

func (s *GCalendarScraper) Name() string {
	return gcalendarSourceName
}

func (s *GCalendarScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	s.resetNotes()
	data, err := fetchURL(ctx, gcalendarURL)
	if err != nil {
		return nil, fmt.Errorf("fetching ICS feed: %w", err)
	}

	stockholm, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		return nil, fmt.Errorf("loading timezone: %w", err)
	}

	events, err := ParseAndExpandICS(string(data), stockholm)
	if err != nil {
		return nil, fmt.Errorf("parsing ICS feed: %w", err)
	}

	var services []model.ChurchService
	skipped := 0
	for _, ev := range events {
		if ev.Cancelled {
			continue
		}

		parish, location, parishLang := matchLocation(ev.Location)
		if parish == "" {
			skipped++
			continue
		}

		svc := model.ChurchService{
			Parish:         parish,
			Source:         gcalendarSourceName,
			SourceURL:      gcalendarSourcePage,
			Date:           ev.Start.Format("2006-01-02"),
			DayOfWeek:      srpska.WeekdayToSwedish(ev.Start.Weekday()),
			ServiceName:    ev.Summary,
			Location:       &location,
			Time:           formatTimeRange(ev),
			Notes:          strPtr(ev.Description),
			ParishLanguage: &parishLang,
		}
		services = append(services, svc)
	}

	if skipped > 0 {
		s.note("parsed %d events → %d matched to known parishes, %d skipped (unknown location)", len(events), len(services), skipped)
	} else {
		s.note("parsed %d events → %d matched to known parishes", len(events), len(services))
	}
	return services, nil
}

// matchLocation returns the parish name, display location, and parish language for an ICS LOCATION string.
// Returns empty strings if the location doesn't match any known parish.
func matchLocation(loc string) (parish, location, parishLanguage string) {
	for _, m := range locationMapping {
		if strings.Contains(loc, m.substring) {
			return m.parish, m.location, m.parishLanguage
		}
	}
	return "", "", ""
}
