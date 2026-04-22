// check-romanian verifies that the /services endpoint contains exactly the
// events from the Romanian Orthodox church's Google Calendar.
//
// Usage:
//
//	go run scripts/check-romanian.go [-url https://ortodoxagudstjanster.se]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	romanianICSURL = "https://calendar.google.com/calendar/ical/e55ade1dbe3651b62babb5e6012c4bde4765646a8932498de709d7816ee026e4@group.calendar.google.com/public/basic.ics"
	romanianParish = "Sankt Göran"
)

// service mirrors the JSON shape returned by /services.
type service struct {
	Parish      string  `json:"parish"`
	Date        string  `json:"date"`
	ServiceName string  `json:"service_name"`
	Time        *string `json:"time"`
}

// eventKey is a comparable identifier for matching events.
type eventKey struct {
	Date        string
	ServiceName string
	Time        string
}

func main() {
	baseURL := flag.String("url", "https://ortodoxagudstjanster.se", "Base URL of the web service")
	flag.Parse()

	loc, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load timezone: %v\n", err)
		os.Exit(1)
	}

	// Fetch events from the Google Calendar ICS feed
	calendarEvents, err := fetchCalendarEvents(loc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to fetch calendar: %v\n", err)
		os.Exit(1)
	}

	// Fetch Romanian events from /services
	apiEvents, err := fetchAPIEvents(*baseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to fetch /services: %v\n", err)
		os.Exit(1)
	}

	// Compare
	calendarSet := make(map[eventKey]bool)
	for _, k := range calendarEvents {
		calendarSet[k] = true
	}
	apiSet := make(map[eventKey]bool)
	for _, k := range apiEvents {
		apiSet[k] = true
	}

	var missingFromAPI []eventKey
	for _, k := range calendarEvents {
		if !apiSet[k] {
			missingFromAPI = append(missingFromAPI, k)
		}
	}

	var extraInAPI []eventKey
	for _, k := range apiEvents {
		if !calendarSet[k] {
			extraInAPI = append(extraInAPI, k)
		}
	}

	sort.Slice(missingFromAPI, func(i, j int) bool {
		return missingFromAPI[i].Date < missingFromAPI[j].Date
	})
	sort.Slice(extraInAPI, func(i, j int) bool {
		return extraInAPI[i].Date < extraInAPI[j].Date
	})

	ok := true
	if len(missingFromAPI) > 0 {
		ok = false
		fmt.Printf("MISSING from /services (%d events in calendar but not in API):\n", len(missingFromAPI))
		for _, k := range missingFromAPI {
			fmt.Printf("  %s %s %s\n", k.Date, k.Time, k.ServiceName)
		}
		fmt.Println()
	}

	if len(extraInAPI) > 0 {
		ok = false
		fmt.Printf("EXTRA in /services (%d events in API but not in calendar):\n", len(extraInAPI))
		for _, k := range extraInAPI {
			fmt.Printf("  %s %s %s\n", k.Date, k.Time, k.ServiceName)
		}
		fmt.Println()
	}

	fmt.Printf("Calendar events: %d\n", len(calendarEvents))
	fmt.Printf("API events (Romanian): %d\n", len(apiEvents))

	if ok {
		fmt.Println("OK — all events match")
	} else {
		os.Exit(1)
	}
}

func fetchCalendarEvents(loc *time.Location) ([]eventKey, error) {
	resp, err := http.Get(romanianICSURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	events, err := parseICS(string(data))
	if err != nil {
		return nil, err
	}

	var keys []eventKey
	for _, ev := range events {
		if ev.cancelled {
			continue
		}
		start, allDay, err := parseICSTimestamp(ev.dtstart, loc)
		if err != nil {
			continue
		}

		date := start.Format("2006-01-02")
		var timeStr string
		if !allDay {
			t := start.Format("15:04")
			if ev.dtend != "" {
				end, endAllDay, err := parseICSTimestamp(ev.dtend, loc)
				if err == nil && !endAllDay {
					timeStr = fmt.Sprintf("%s - %s", t, end.Format("15:04"))
				} else {
					timeStr = t
				}
			} else {
				timeStr = t
			}
		}

		keys = append(keys, eventKey{
			Date:        date,
			ServiceName: ev.summary,
			Time:        timeStr,
		})
	}
	return keys, nil
}

func fetchAPIEvents(baseURL string) ([]eventKey, error) {
	resp, err := http.Get(baseURL + "/services")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var services []service
	if err := json.NewDecoder(resp.Body).Decode(&services); err != nil {
		return nil, err
	}

	var keys []eventKey
	for _, s := range services {
		if s.Parish != romanianParish {
			continue
		}
		timeStr := ""
		if s.Time != nil {
			timeStr = *s.Time
		}
		keys = append(keys, eventKey{
			Date:        s.Date,
			ServiceName: s.ServiceName,
			Time:        timeStr,
		})
	}
	return keys, nil
}

// --- Minimal ICS parser (self-contained, mirrors internal/scraper logic) ---

type icsEvent struct {
	summary      string
	dtstart      string
	dtend        string
	description  string
	cancelled    bool
	rrule        string
	exdates      []string
	uid          string
	recurrenceID string
}

func parseICS(data string) ([]icsEvent, error) {
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
			sl := strings.ToLower(current.summary)
			if strings.Contains(sl, "cancelled") || strings.Contains(sl, "canceled") {
				current.cancelled = true
			}
			events = append(events, *current)
			current = nil
			continue
		}
		if current == nil {
			continue
		}

		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		nameParams := line[:colonIdx]
		value := unescapeICS(line[colonIdx+1:])

		name := nameParams
		if semiIdx := strings.Index(nameParams, ";"); semiIdx >= 0 {
			name = nameParams[:semiIdx]
		}

		switch name {
		case "SUMMARY":
			current.summary = value
		case "DTSTART":
			current.dtstart = line[colonIdx+1:]
		case "DTEND":
			current.dtend = line[colonIdx+1:]
		case "DESCRIPTION":
			current.description = value
		case "RRULE":
			current.rrule = value
		case "EXDATE":
			current.exdates = append(current.exdates, line[colonIdx+1:])
		case "UID":
			current.uid = value
		case "RECURRENCE-ID":
			current.recurrenceID = line[colonIdx+1:]
		case "STATUS":
			if strings.EqualFold(value, "CANCELLED") {
				current.cancelled = true
			}
		}
	}
	return events, nil
}

func unescapeICS(s string) string {
	s = strings.ReplaceAll(s, "\\n", "\n")
	s = strings.ReplaceAll(s, "\\N", "\n")
	s = strings.ReplaceAll(s, "\\,", ",")
	s = strings.ReplaceAll(s, "\\;", ";")
	s = strings.ReplaceAll(s, "\\\\", "\\")
	return strings.TrimSpace(s)
}

func parseICSTimestamp(ts string, loc *time.Location) (time.Time, bool, error) {
	ts = strings.TrimSpace(ts)
	if ts == "" {
		return time.Time{}, false, fmt.Errorf("empty timestamp")
	}
	if len(ts) == 8 {
		t, err := time.ParseInLocation("20060102", ts, loc)
		return t, true, err
	}
	if strings.HasSuffix(ts, "Z") {
		t, err := time.Parse("20060102T150405Z", ts)
		if err != nil {
			return time.Time{}, false, err
		}
		return t.In(loc), false, nil
	}
	t, err := time.ParseInLocation("20060102T150405", ts, loc)
	return t, false, err
}
