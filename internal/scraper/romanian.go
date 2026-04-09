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
	romanianSourceName   = "Sankt Göran"
	romanianICSURL       = "https://calendar.google.com/calendar/ical/e55ade1dbe3651b62babb5e6012c4bde4765646a8932498de709d7816ee026e4@group.calendar.google.com/public/basic.ics"
	romanianCalendarPage = "https://calendar.google.com/calendar/embed?src=e55ade1dbe3651b62babb5e6012c4bde4765646a8932498de709d7816ee026e4%40group.calendar.google.com&ctz=Europe%2FStockholm"
	romanianCalendarName = "Google Calendar (Rumänska Ortodoxa Kyrkan)"
	romanianLocation     = "Matteus Lillkyrkan, Vanadisvägen 35, 113 23 Stockholm"
	romanianLanguage     = "Rumänska"
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

	events, err := parseICS(string(data))
	if err != nil {
		return nil, fmt.Errorf("parsing ICS feed: %w", err)
	}

	stockholm, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		return nil, fmt.Errorf("loading timezone: %w", err)
	}

	location := romanianLocation
	lang := romanianLanguage

	var services []model.ChurchService
	for _, ev := range events {
		if ev.cancelled {
			continue
		}

		start, allDay, err := parseICSTimestamp(ev.dtstart, stockholm)
		if err != nil {
			continue
		}

		date := start.Format("2006-01-02")
		dayOfWeek := srpska.WeekdayToSwedish(start.Weekday())

		var timeStr *string
		if !allDay {
			t := start.Format("15:04")
			if ev.dtend != "" {
				end, endAllDay, err := parseICSTimestamp(ev.dtend, stockholm)
				if err == nil && !endAllDay {
					r := fmt.Sprintf("%s - %s", t, end.Format("15:04"))
					timeStr = &r
				} else {
					timeStr = &t
				}
			} else {
				timeStr = &t
			}
		}

		var notes *string
		if ev.description != "" {
			notes = &ev.description
		}

		eventLang := parseRomanianEventLanguage(ev.summary)

		svc := model.ChurchService{
			Parish:         romanianSourceName,
			Source:         romanianCalendarName,
			SourceURL:      romanianCalendarPage,
			Date:           date,
			DayOfWeek:      dayOfWeek,
			ServiceName:    ev.summary,
			Location:       &location,
			Time:           timeStr,
			Notes:          notes,
			ParishLanguage: &lang,
			EventLanguage:  eventLang,
		}
		services = append(services, svc)
	}

	return services, nil
}

// knownLanguages maps lowercase language name suffixes to their canonical form.
var knownLanguages = map[string]string{
	"rumänska":    "Rumänska",
	"română":      "Rumänska",
	"svenska":     "Svenska",
	"engelska":    "Engelska",
	"grekiska":    "Grekiska",
	"kyrkslaviska": "Kyrkoslaviska",
	"kyrkoslaviska": "Kyrkoslaviska",
}

// parseRomanianEventLanguage detects a language suffix like "- Rumänska" in an
// event name and returns the canonical language string, or nil if not found.
func parseRomanianEventLanguage(name string) *string {
	if idx := strings.LastIndex(name, " - "); idx >= 0 {
		suffix := strings.ToLower(strings.TrimSpace(name[idx+3:]))
		if canonical, ok := knownLanguages[suffix]; ok {
			return &canonical
		}
	}
	return nil
}
