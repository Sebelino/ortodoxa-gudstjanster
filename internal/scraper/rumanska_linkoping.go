package scraper

import (
	"context"
	"fmt"
	"time"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/srpska"
)

const (
	rumanskaLkpSourceName = "Google Calendar (Rumänska ortodoxa kyrkan i Linköping)"
	rumanskaLkpURL        = "https://calendar.google.com/calendar/ical/818aab2c208b29d8e29a7fad4bb3b03b4a3cd1ce458ffcf17506d7b0ec13f165@group.calendar.google.com/public/basic.ics"
	rumanskaLkpSourcePage = "https://calendar.google.com/calendar/embed?src=818aab2c208b29d8e29a7fad4bb3b03b4a3cd1ce458ffcf17506d7b0ec13f165%40group.calendar.google.com&ctz=Europe%2FStockholm"
	rumanskaLkpParish     = "Rumänska ortodoxa kyrkan i Linköping"
)

type RumanskaLinkopingScraper struct{ NoteCollector }

func NewRumanskaLinkopingScraper() *RumanskaLinkopingScraper {
	return &RumanskaLinkopingScraper{}
}

func (s *RumanskaLinkopingScraper) Name() string {
	return rumanskaLkpSourceName
}

func (s *RumanskaLinkopingScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	s.resetNotes()
	data, err := fetchURL(ctx, rumanskaLkpURL)
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
	for _, ev := range events {
		if ev.Cancelled {
			continue
		}

		desc := htmlBrRE.ReplaceAllString(ev.Description, "\n")
		desc = htmlTagRE.ReplaceAllString(desc, "")

		language := firstSubmatch(gcalManualLanguageRE, desc)
		descField := firstSubmatch(gcalManualDescRE, desc)
		if descField == "" {
			descField = stripStructuredFields(desc)
		}

		svc := model.ChurchService{
			Parish:        rumanskaLkpParish,
			Source:        rumanskaLkpSourceName,
			SourceURL:     rumanskaLkpSourcePage,
			Date:          ev.Start.Format("2006-01-02"),
			DayOfWeek:     srpska.WeekdayToSwedish(ev.Start.Weekday()),
			ServiceName:   ev.Summary,
			Location:      strPtr(ev.Location),
			Time:          formatTimeRange(ev),
			Notes:         strPtr(descField),
			EventLanguage: strPtr(language),
		}
		services = append(services, svc)
	}

	s.note("parsed %d events → %d services", len(events), len(services))
	return services, nil
}
