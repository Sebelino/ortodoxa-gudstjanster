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
	{"Petruskyrkan", parishHeligaAnna, "Petruskyrkan, Kyrkvägen 27, Stocksund", "Svenska"},
	{"Heliga Annas Ortodoxa", parishHeligaAnna, "Heliga Annas Ortodoxa kyrkoförsamling, Kyrkvägen 27, Stocksund", "Svenska"},
	{"Sankt Ignatios Folkhögskola", parishStIgnatios, "Sankt Ignatios Folkhögskola, Nygatan 2, Södertälje", "Svenska, grekiska, serbiska"},
	{"Sankt Ignatios andliga akademi", parishStIgnatios, "Sankt Ignatios andliga akademi, Nygatan 2, Södertälje", "Svenska, grekiska, serbiska"},
	// Catch-all for Södertälje addresses not matching above
	{"Södertälje", parishStIgnatios, "Nygatan 2, Södertälje", "Svenska, grekiska, serbiska"},
}

// GCalendarScraper fetches events from a public Google Calendar ICS feed.
type GCalendarScraper struct{}

func NewGCalendarScraper() *GCalendarScraper {
	return &GCalendarScraper{}
}

func (s *GCalendarScraper) Name() string {
	return gcalendarSourceName
}

func (s *GCalendarScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	data, err := fetchURL(ctx, gcalendarURL)
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

	var services []model.ChurchService
	for _, ev := range events {
		// Skip cancelled events
		if ev.cancelled {
			continue
		}

		// Map location to parish
		parish, location, parishLang := matchLocation(ev.location)
		if parish == "" {
			continue
		}

		// Parse start time
		start, allDay, err := parseICSTimestamp(ev.dtstart, stockholm)
		if err != nil {
			continue
		}

		date := start.Format("2006-01-02")
		dayOfWeek := srpska.WeekdayToSwedish(start.Weekday())

		var timeStr *string
		if !allDay {
			t := start.Format("15:04")
			// If we have an end time, include it
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

		serviceName := ev.summary

		var notes *string
		if ev.description != "" {
			notes = &ev.description
		}

		svc := model.ChurchService{
			Parish:         parish,
			Source:         gcalendarSourceName,
			SourceURL:      gcalendarSourcePage,
			Date:           date,
			DayOfWeek:      dayOfWeek,
			ServiceName:    serviceName,
			Location:       &location,
			Time:           timeStr,
			Notes:          notes,
			ParishLanguage: &parishLang,
		}
		services = append(services, svc)
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

// icsEvent represents a parsed VEVENT from an ICS feed.
type icsEvent struct {
	summary     string
	dtstart     string
	dtend       string
	location    string
	description string
	cancelled   bool
}

// parseICS parses an ICS feed into a slice of events.
func parseICS(data string) ([]icsEvent, error) {
	// Unfold continuation lines (RFC 5545: lines starting with space/tab
	// are continuations of the previous line)
	data = strings.ReplaceAll(data, "\r\n", "\n")
	data = strings.ReplaceAll(data, "\n ", "")
	data = strings.ReplaceAll(data, "\n\t", "")

	lines := strings.Split(data, "\n")

	var events []icsEvent
	var current *icsEvent

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")

		if line == "BEGIN:VEVENT" {
			current = &icsEvent{}
			continue
		}
		if line == "END:VEVENT" && current != nil {
			// Check for cancelled in summary
			summaryLower := strings.ToLower(current.summary)
			if strings.Contains(summaryLower, "cancelled") || strings.Contains(summaryLower, "canceled") {
				current.cancelled = true
			}
			events = append(events, *current)
			current = nil
			continue
		}
		if current == nil {
			continue
		}

		// Parse property: NAME;params:value or NAME:value
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		nameParams := line[:colonIdx]
		value := unescapeICS(line[colonIdx+1:])

		// Strip parameters (e.g., DTSTART;TZID=Europe/Stockholm)
		name := nameParams
		if semiIdx := strings.Index(nameParams, ";"); semiIdx >= 0 {
			name = nameParams[:semiIdx]
		}

		switch name {
		case "SUMMARY":
			current.summary = value
		case "DTSTART":
			current.dtstart = line[colonIdx+1:] // keep raw for timestamp parsing
		case "DTEND":
			current.dtend = line[colonIdx+1:]
		case "LOCATION":
			current.location = value
		case "DESCRIPTION":
			current.description = value
		case "STATUS":
			if strings.EqualFold(value, "CANCELLED") {
				current.cancelled = true
			}
		}
	}

	return events, nil
}

// unescapeICS reverses ICS escaping: \n → newline, \, → comma, \; → semicolon, \\ → backslash.
func unescapeICS(s string) string {
	s = strings.ReplaceAll(s, "\\n", "\n")
	s = strings.ReplaceAll(s, "\\N", "\n")
	s = strings.ReplaceAll(s, "\\,", ",")
	s = strings.ReplaceAll(s, "\\;", ";")
	s = strings.ReplaceAll(s, "\\\\", "\\")
	return strings.TrimSpace(s)
}

// parseICSTimestamp parses an ICS timestamp string into a time.Time.
// Returns (time, allDay, error). Handles formats:
//   - 20060102T150405Z (UTC)
//   - 20060102T150405  (local, interpreted as Stockholm)
//   - 20060102         (all-day event)
func parseICSTimestamp(ts string, loc *time.Location) (time.Time, bool, error) {
	ts = strings.TrimSpace(ts)
	if ts == "" {
		return time.Time{}, false, fmt.Errorf("empty timestamp")
	}

	// All-day: YYYYMMDD
	if len(ts) == 8 {
		t, err := time.ParseInLocation("20060102", ts, loc)
		return t, true, err
	}

	// UTC: YYYYMMDDTHHMMSSZ
	if strings.HasSuffix(ts, "Z") {
		t, err := time.Parse("20060102T150405Z", ts)
		if err != nil {
			return time.Time{}, false, err
		}
		return t.In(loc), false, nil
	}

	// Local: YYYYMMDDTHHMMSS
	t, err := time.ParseInLocation("20060102T150405", ts, loc)
	return t, false, err
}
