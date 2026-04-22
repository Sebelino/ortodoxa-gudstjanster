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
	{"Petruskyrkan", parishHeligaAnna, "Heliga Anna, Kyrkvägen 27, 182 74 Stocksund, Sweden", "Svenska"},
	{"Heliga Annas Ortodoxa", parishHeligaAnna, "Heliga Anna, Kyrkvägen 27, 182 74 Stocksund, Sweden", "Svenska"},
	{"Sankt Ignatios Folkhögskola", parishStIgnatios, "Sankt Ignatios Folkhögskola, Nygatan 2, 151 72 Södertälje", "Svenska, grekiska, serbiska"},
	{"Sankt Ignatios andliga akademi", parishStIgnatios, "Sankt Ignatios andliga akademi, Nygatan 2, 151 72 Södertälje", "Svenska, grekiska, serbiska"},
	// Catch-all for Södertälje addresses not matching above
	{"Södertälje", parishStIgnatios, "Nygatan 2, 151 72 Södertälje", "Svenska, grekiska, serbiska"},
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
	summary      string
	dtstart      string
	dtend        string
	location     string
	description  string
	cancelled    bool
	rrule        string   // raw RRULE value, e.g. "FREQ=WEEKLY;BYDAY=TH"
	exdates      []string // raw EXDATE values for exclusions
	uid          string   // UID for correlating overrides with recurring series
	recurrenceID string   // raw RECURRENCE-ID value (marks this as an override)
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
		case "RRULE":
			current.rrule = value
		case "EXDATE":
			current.exdates = append(current.exdates, line[colonIdx+1:])
		case "UID":
			current.uid = value
		case "RECURRENCE-ID":
			current.recurrenceID = line[colonIdx+1:] // keep raw for timestamp parsing
		case "STATUS":
			if strings.EqualFold(value, "CANCELLED") {
				current.cancelled = true
			}
		}
	}

	return events, nil
}

// defaultRecurringHorizon is how far into the future recurring events are expanded.
const defaultRecurringHorizon = 26 * 7 * 24 * time.Hour // 26 weeks

// expandRecurringEvents expands any events with an RRULE into individual
// occurrences up to the given horizon from now. Non-recurring events pass
// through unchanged. Events with a RECURRENCE-ID override a specific
// occurrence of the recurring series they belong to (matched by UID).
func expandRecurringEvents(events []icsEvent, loc *time.Location) []icsEvent {
	now := time.Now()
	horizon := now.Add(defaultRecurringHorizon)

	// Build override map: uid+date → override event
	overrides := make(map[string]icsEvent)
	for _, ev := range events {
		if ev.recurrenceID == "" {
			continue
		}
		if t, _, err := parseICSTimestamp(ev.recurrenceID, loc); err == nil {
			key := ev.uid + "|" + t.Format("2006-01-02")
			overrides[key] = ev
		}
	}

	var out []icsEvent
	for _, ev := range events {
		// Skip override events — they are applied during expansion
		if ev.recurrenceID != "" {
			continue
		}
		if ev.rrule == "" {
			out = append(out, ev)
			continue
		}

		start, allDay, err := parseICSTimestamp(ev.dtstart, loc)
		if err != nil {
			out = append(out, ev) // can't parse, keep as-is
			continue
		}

		// Compute event duration from DTSTART/DTEND
		var duration time.Duration
		if ev.dtend != "" {
			end, _, err := parseICSTimestamp(ev.dtend, loc)
			if err == nil {
				duration = end.Sub(start)
			}
		}

		// Parse RRULE parameters
		params := parseRRuleParams(ev.rrule)
		freq := params["FREQ"]
		if freq != "DAILY" && freq != "WEEKLY" {
			out = append(out, ev) // unsupported frequency, keep original
			continue
		}

		interval := 1
		if v, ok := params["INTERVAL"]; ok {
			if n, err := fmt.Sscanf(v, "%d", &interval); n != 1 || err != nil {
				interval = 1
			}
		}

		maxCount := -1
		if v, ok := params["COUNT"]; ok {
			fmt.Sscanf(v, "%d", &maxCount)
		}

		var until time.Time
		if v, ok := params["UNTIL"]; ok {
			if t, _, err := parseICSTimestamp(v, loc); err == nil {
				until = t
			}
		}

		// Build EXDATE set
		exdateSet := make(map[string]bool)
		for _, exd := range ev.exdates {
			if t, _, err := parseICSTimestamp(exd, loc); err == nil {
				exdateSet[t.Format("2006-01-02")] = true
			}
		}

		// Generate occurrences
		count := 0
		for current := start; !current.After(horizon); {
			if !until.IsZero() && current.After(until) {
				break
			}
			if maxCount >= 0 && count >= maxCount {
				break
			}

			dateStr := current.Format("2006-01-02")
			if !exdateSet[dateStr] {
				// Check for a RECURRENCE-ID override for this occurrence
				overrideKey := ev.uid + "|" + dateStr
				if ov, ok := overrides[overrideKey]; ok {
					// Use the override event instead
					ov.recurrenceID = "" // clear so it's treated as normal
					out = append(out, ov)
				} else {
					inst := ev
					inst.rrule = "" // mark as expanded
					inst.exdates = nil
					if allDay {
						inst.dtstart = current.Format("20060102")
						if duration > 0 {
							inst.dtend = current.Add(duration).Format("20060102")
						}
					} else {
						inst.dtstart = current.Format("20060102T150405")
						if duration > 0 {
							inst.dtend = current.Add(duration).Format("20060102T150405")
						}
					}
					out = append(out, inst)
				}
			}

			count++
			switch freq {
			case "DAILY":
				current = current.AddDate(0, 0, interval)
			case "WEEKLY":
				current = current.AddDate(0, 0, 7*interval)
			}
		}
	}

	return out
}

// parseRRuleParams splits an RRULE value like "FREQ=WEEKLY;BYDAY=TH;COUNT=10"
// into a map.
func parseRRuleParams(rrule string) map[string]string {
	params := make(map[string]string)
	for _, part := range strings.Split(rrule, ";") {
		if eqIdx := strings.Index(part, "="); eqIdx >= 0 {
			params[part[:eqIdx]] = part[eqIdx+1:]
		}
	}
	return params
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
