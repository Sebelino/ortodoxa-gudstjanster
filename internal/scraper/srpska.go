package scraper

import (
	"context"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/srpska"
)

const (
	srpskaSourceName = "Sankt Sava"
	srpskaLocation   = "Stockholm, Bägerstavägen 68"
	srpskaLanguage   = "Kyrkoslaviska"
	srpskaWeeks      = 26
)

// SrpskaScraper scrapes the Serbian Orthodox Church schedule.
type SrpskaScraper struct{}

// NewSrpskaScraper creates a new scraper for the Serbian Orthodox Church.
func NewSrpskaScraper() *SrpskaScraper {
	return &SrpskaScraper{}
}

func (s *SrpskaScraper) Name() string {
	return srpskaSourceName
}

func (s *SrpskaScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	// Part 1: Fetch raw table text from the website
	tableText, err := srpska.FetchScheduleTable(ctx)
	if err != nil {
		return nil, err
	}

	// Part 2: Parse into structured schedule
	schedule, err := srpska.ParseScheduleTable(tableText)
	if err != nil {
		return nil, err
	}

	// Part 3: Generate calendar events
	events := srpska.GenerateEvents(schedule, srpskaWeeks)

	// Convert to ChurchService model
	return s.toChurchServices(events), nil
}

func (s *SrpskaScraper) toChurchServices(events []srpska.CalendarEvent) []model.ChurchService {
	services := make([]model.ChurchService, len(events))
	location := srpskaLocation
	lang := srpskaLanguage

	for i, event := range events {
		timeStr := event.Time
		services[i] = model.ChurchService{
			Source:      srpskaSourceName,
			SourceURL:   srpska.CalendarURL,
			Date:        event.Date,
			DayOfWeek:   event.DayOfWeek,
			ServiceName: event.ServiceName,
			Location:    &location,
			Time:        &timeStr,
			Occasion:    nil,
			Notes:       nil,
			Language:    &lang,
		}
	}

	return services
}
