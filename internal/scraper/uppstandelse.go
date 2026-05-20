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
	uppstandelseParish     = "Kristi Uppståndelses Ortodoxa församling"
	uppstandelseParishLang = "Svenska"
)

var uppstandelseLocationMapping = []struct {
	substring string
	location  string
}{
	{"Sannaplan", "St: Matteus kapell på Västra Kyrkogården, Sannaplan 1, 414 74 Göteborg"},
	{"Runstavsgatan", "Stefan Dečanskis kyrka, Runstavsgatan 9, 415 08 Göteborg"},
}

type UppstandelseScraper struct{}

func NewUppstandelseScraper() *UppstandelseScraper {
	return &UppstandelseScraper{}
}

func (s *UppstandelseScraper) Name() string {
	return uppstandelseSourceName
}

func (s *UppstandelseScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	data, err := fetchURL(ctx, uppstandelseURL)
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

	events = expandRecurringEvents(events, stockholm)

	parishLang := uppstandelseParishLang
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

		var location *string
		if ev.location != "" {
			loc := ev.location
			for _, m := range uppstandelseLocationMapping {
				if strings.Contains(ev.location, m.substring) {
					loc = m.location
					break
				}
			}
			location = &loc
		}

		var notes *string
		if ev.description != "" {
			notes = &ev.description
		}

		svc := model.ChurchService{
			Parish:         uppstandelseParish,
			Source:         uppstandelseSourceName,
			SourceURL:      uppstandelseSourcePage,
			Date:           date,
			DayOfWeek:      dayOfWeek,
			ServiceName:    ev.summary,
			Location:       location,
			Time:           timeStr,
			Notes:          notes,
			ParishLanguage: &parishLang,
		}
		services = append(services, svc)
	}

	return services, nil
}
