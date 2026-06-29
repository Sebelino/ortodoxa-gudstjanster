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
	uppstandelseSourceName = "Kristi Uppståndelses Ortodoxa församling"
	uppstandelseURL        = "https://calendar.google.com/calendar/ical/9s8no8slakq0m15ft6d3b8a9m4@group.calendar.google.com/public/basic.ics"
	uppstandelseSourcePage = "https://calendar.google.com/calendar/u/0/embed?src=9s8no8slakq0m15ft6d3b8a9m4@group.calendar.google.com&ctz=Europe/Stockholm"
	uppstandelseParishSlug = "kristi-uppstandelse"
	uppstandelseParishLang = "Svenska"
)

var uppstandelseLocationMapping = []struct {
	substring string
	location  string
}{
	{"Sannaplan", "St: Matteus kapell på Västra Kyrkogården, Sannaplan 1, 414 74 Göteborg"},
	{"Runstavsgatan", "Stefan Dečanskis kyrka, Runstavsgatan 9, 415 08 Göteborg"},
}

type UppstandelseScraper struct{ NoteCollector }

func NewUppstandelseScraper() *UppstandelseScraper {
	return &UppstandelseScraper{}
}

func (s *UppstandelseScraper) Name() string {
	return uppstandelseSourceName
}

func (s *UppstandelseScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	s.resetNotes()
	data, err := fetchURL(ctx, uppstandelseURL)
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

	parishLang := uppstandelseParishLang
	var services []model.ChurchService
	for _, ev := range events {
		if ev.Cancelled {
			continue
		}

		var location *string
		if ev.Location != "" {
			loc := ev.Location
			for _, m := range uppstandelseLocationMapping {
				if strings.Contains(ev.Location, m.substring) {
					loc = m.location
					break
				}
			}
			location = &loc
		}

		svc := model.ChurchService{
			Parish:         "",
			ParishSlug:     uppstandelseParishSlug,
			Source:         uppstandelseSourceName,
			SourceURL:      uppstandelseSourcePage,
			Date:           ev.Start.Format("2006-01-02"),
			DayOfWeek:      srpska.WeekdayToSwedish(ev.Start.Weekday()),
			ServiceName:    ev.Summary,
			Location:       location,
			Time:           formatTimeRange(ev),
			Notes:          strPtr(ev.Description),
			ParishLanguage: &parishLang,
		}
		services = append(services, svc)
	}

	s.note("parsed %d events → %d active services", len(events), len(services))
	return services, nil
}
