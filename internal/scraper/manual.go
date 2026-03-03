package scraper

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/store"
)

const manualScraperName = "Manuella händelser"

// RecurringEvent defines a manually configured recurring event.
type RecurringEvent struct {
	Source        string `json:"source"`
	ServiceName   string `json:"service_name"`
	Location      string `json:"location"`
	Time          string `json:"time"`
	Language      string `json:"language"`
	StartDate     string `json:"start_date"` // "2006-01-02"
	IntervalWeeks int    `json:"interval_weeks"`
}

// ManualScraper generates events from recurring event definitions stored in GCS.
type ManualScraper struct {
	reader *store.BucketReader
}

// NewManualScraper creates a new manual scraper.
// If reader is nil, Fetch returns an empty slice.
func NewManualScraper(reader *store.BucketReader) *ManualScraper {
	return &ManualScraper{reader: reader}
}

func (s *ManualScraper) Name() string {
	return manualScraperName
}

func (s *ManualScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	if s.reader == nil {
		log.Printf("Manual scraper: no bucket reader configured, returning empty")
		return nil, nil
	}

	data, err := s.reader.ReadObject(ctx, "events.json")
	if err != nil {
		log.Printf("Manual scraper: failed to read events.json: %v", err)
		return nil, nil
	}

	var events []RecurringEvent
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, err
	}

	var services []model.ChurchService

	for _, event := range events {
		startDate, err := time.Parse("2006-01-02", event.StartDate)
		if err != nil {
			return nil, err
		}

		interval := time.Duration(event.IntervalWeeks) * 7 * 24 * time.Hour
		endDate := startDate.AddDate(0, 0, 26*7)

		for date := startDate; !date.After(endDate); date = date.Add(interval) {
			location := event.Location
			timeStr := event.Time
			language := event.Language
			svc := model.ChurchService{
				Source:      event.Source,
				Date:        date.Format("2006-01-02"),
				DayOfWeek:   swedishDayOfWeek(date.Weekday()),
				ServiceName: event.ServiceName,
				Location:    &location,
				Time:        &timeStr,
				Language:    &language,
			}
			services = append(services, svc)
		}
	}

	return services, nil
}

func swedishDayOfWeek(day time.Weekday) string {
	switch day {
	case time.Monday:
		return "Måndag"
	case time.Tuesday:
		return "Tisdag"
	case time.Wednesday:
		return "Onsdag"
	case time.Thursday:
		return "Torsdag"
	case time.Friday:
		return "Fredag"
	case time.Saturday:
		return "Lördag"
	case time.Sunday:
		return "Söndag"
	default:
		return ""
	}
}
