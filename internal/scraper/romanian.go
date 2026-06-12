package scraper

import (
	"context"
	"fmt"
	"time"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/srpska"
)

const (
	romanianSourceName   = "Sankt Göran"
	romanianParishSlug   = "sankt-goran"
	romanianICSURL       = "https://calendar.google.com/calendar/ical/e55ade1dbe3651b62babb5e6012c4bde4765646a8932498de709d7816ee026e4@group.calendar.google.com/public/basic.ics"
	romanianCalendarPage = "https://calendar.google.com/calendar/embed?src=e55ade1dbe3651b62babb5e6012c4bde4765646a8932498de709d7816ee026e4%40group.calendar.google.com&ctz=Europe%2FStockholm"
	romanianCalendarName = "Google Calendar (Rumänska Ortodoxa Kyrkan)"
	romanianLocation     = "Matteus Lillkyrkan, Vanadisvägen 35, 113 23 Stockholm"
	romanianLanguage     = "Rumänska, svenska, engelska"
)

// RomanianScraper fetches events from the Romanian Orthodox church Sankt Göran's Google Calendar.
type RomanianScraper struct{}

func NewRomanianScraper() *RomanianScraper {
	return &RomanianScraper{}
}

func (s *RomanianScraper) Name() string {
	return romanianSourceName
}

func (s *RomanianScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	data, err := fetchURL(ctx, romanianICSURL)
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

	lang := romanianLanguage

	var services []model.ChurchService
	for _, ev := range events {
		if ev.Cancelled {
			continue
		}

		location := romanianLocation
		if ev.Location != "" {
			location = ev.Location
		}

		svc := model.ChurchService{
			Parish:         "",
			ParishSlug:     romanianParishSlug,
			Source:         romanianCalendarName,
			SourceURL:      romanianCalendarPage,
			Date:           ev.Start.Format("2006-01-02"),
			DayOfWeek:      srpska.WeekdayToSwedish(ev.Start.Weekday()),
			ServiceName:    ev.Summary,
			Location:       &location,
			Time:           formatTimeRange(ev),
			Notes:          strPtr(ev.Description),
			ParishLanguage: &lang,
		}
		services = append(services, svc)
	}

	return services, nil
}
