package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/srpska"
	"ortodoxa-gudstjanster/internal/store"
)

const manualScraperName = "Manuella händelser"

// RecurringEvent defines a manually configured recurring event.
type RecurringEvent struct {
	Parish         string `json:"parish"`
	Source         string `json:"source"`
	SourceURL      string `json:"source_url,omitempty"`
	ServiceName    string `json:"service_name"`
	Title          string `json:"title,omitempty"`
	Location       string `json:"location"`
	StartTimeStr   string `json:"start_time,omitempty"`
	EndTimeStr     string `json:"end_time,omitempty"`
	Language       string `json:"language"`
	ParishLanguage string `json:"parish_language,omitempty"`
	EventLanguage  string `json:"event_language,omitempty"`
	Notes          string `json:"notes,omitempty"`
	StartDate      string `json:"start_date"`            // "2006-01-02"
	EndDate        string `json:"end_date,omitempty"`    // "2006-01-02" (optional, defaults to start_date + 26 weeks)
	IntervalWeeks  int    `json:"interval_weeks"`
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

		if event.IntervalWeeks == 0 {
			// One-time event
			services = append(services, buildManualService(event, startDate))
		} else if event.IntervalWeeks > 0 && event.IntervalWeeks <= 52 {
			// Recurring event
			interval := time.Duration(event.IntervalWeeks) * 7 * 24 * time.Hour
			endDate := startDate.AddDate(0, 0, 26*7)
			if event.EndDate != "" {
				endDate, err = time.Parse("2006-01-02", event.EndDate)
				if err != nil {
					return nil, fmt.Errorf("event %q has invalid end_date: %w", event.ServiceName, err)
				}
			}
			for date := startDate; !date.After(endDate); date = date.Add(interval) {
				services = append(services, buildManualService(event, date))
			}
		} else {
			return nil, fmt.Errorf("event %q has invalid interval_weeks: %d", event.ServiceName, event.IntervalWeeks)
		}
	}

	return services, nil
}

// buildManualService converts a RecurringEvent and a specific date into a ChurchService.
func buildManualService(event RecurringEvent, date time.Time) model.ChurchService {
	location := event.Location
	language := event.Language
	svc := model.ChurchService{
		Parish:      event.Parish,
		Source:      event.Source,
		SourceURL:   event.SourceURL,
		Date:        date.Format("2006-01-02"),
		DayOfWeek:   srpska.WeekdayToSwedish(date.Weekday()),
		ServiceName: event.ServiceName,
		Title:       event.Title,
		Location:    &location,
		Language:    &language,
	}
	if event.ParishLanguage != "" {
		pl := event.ParishLanguage
		svc.ParishLanguage = &pl
	}
	if event.EventLanguage != "" {
		el := event.EventLanguage
		svc.EventLanguage = &el
	}
	if event.Notes != "" {
		notes := event.Notes
		svc.Notes = &notes
	}
	if event.StartTimeStr != "" {
		timeStr := event.StartTimeStr
		svc.Time = &timeStr
		if t, err := parseHHMM(date, event.StartTimeStr); err == nil {
			svc.StartTime = &t
		}
	}
	if event.EndTimeStr != "" {
		if t, err := parseHHMM(date, event.EndTimeStr); err == nil {
			svc.EndTime = &t
		}
	}
	return svc
}

var stockholm *time.Location

func init() {
	var err error
	stockholm, err = time.LoadLocation("Europe/Stockholm")
	if err != nil {
		panic(fmt.Sprintf("failed to load Europe/Stockholm timezone: %v", err))
	}
}

func parseHHMM(date time.Time, s string) (time.Time, error) {
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
		return time.Time{}, fmt.Errorf("invalid time format %q: %w", s, err)
	}
	return time.Date(date.Year(), date.Month(), date.Day(), h, m, 0, 0, stockholm), nil
}

