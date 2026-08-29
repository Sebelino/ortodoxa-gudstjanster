package scraper

import (
	"context"
	"fmt"
	"time"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/srpska"
)

const (
	sigfridSourceName = "Google Calendar (Helige Sigfrid och Rafael)"
	sigfridURL        = "https://calendar.google.com/calendar/ical/46fe913e02039ea18cb997c35968e0b63bd3da3559e97f9b5e1b607169368a4a@group.calendar.google.com/public/basic.ics"
	sigfridSourcePage = "https://calendar.google.com/calendar/embed?src=46fe913e02039ea18cb997c35968e0b63bd3da3559e97f9b5e1b607169368a4a%40group.calendar.google.com&ctz=Europe%2FStockholm"
	sigfridParish     = "Helige Sigfrid och Rafael"
)

type SigfridScraper struct{ NoteCollector }

func NewSigfridScraper() *SigfridScraper {
	return &SigfridScraper{}
}

func (s *SigfridScraper) AllowDecrease() bool { return true }

func (s *SigfridScraper) Name() string {
	return sigfridSourceName
}

func (s *SigfridScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	s.resetNotes()
	data, err := fetchURL(ctx, sigfridURL)
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
			Parish:        sigfridParish,
			Source:        sigfridSourceName,
			SourceURL:     sigfridSourcePage,
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
