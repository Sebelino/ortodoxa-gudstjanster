package scraper

import (
	"fmt"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"
	"github.com/teambition/rrule-go"
)

const defaultExpandHorizon = 52 * 7 * 24 * time.Hour // 52 weeks

// ExpandedEvent is a single calendar occurrence with parsed timestamps.
type ExpandedEvent struct {
	Summary     string
	Location    string
	Description string
	Start       time.Time
	End         time.Time
	AllDay      bool
	Cancelled   bool
}

// ParseAndExpandICS parses an ICS feed, expands recurring events, and returns
// individual occurrences with resolved RECURRENCE-ID overrides and EXDATE exclusions.
func ParseAndExpandICS(data string, loc *time.Location) ([]ExpandedEvent, error) {
	cal, err := ics.ParseCalendar(strings.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parsing ICS: %w", err)
	}

	now := time.Now()
	horizon := now.Add(defaultExpandHorizon)

	vevents := cal.Events()

	// Separate override events (RECURRENCE-ID) from base events.
	type overrideKey struct {
		uid  string
		date string
	}
	overrides := make(map[overrideKey]*ics.VEvent)
	var baseEvents []*ics.VEvent

	for _, ev := range vevents {
		if prop := ev.GetProperty(ics.ComponentPropertyRecurrenceId); prop != nil {
			uid := propValue(ev, ics.ComponentPropertyUniqueId)
			t, _, err := parseRawTimestamp(prop.Value, loc)
			if err == nil {
				overrides[overrideKey{uid, t.Format("2006-01-02")}] = ev
			}
			continue
		}
		baseEvents = append(baseEvents, ev)
	}

	var out []ExpandedEvent
	for _, ev := range baseEvents {
		cancelled := isCancelled(ev)

		start, allDay, err := getEventStart(ev, loc)
		if err != nil {
			continue
		}
		end, _, _ := getEventEnd(ev, loc)
		duration := end.Sub(start)

		rruleProp := propValue(ev, ics.ComponentPropertyRrule)
		if rruleProp == "" {
			// Non-recurring event
			out = append(out, makeExpandedEvent(ev, start, end, allDay, cancelled))
			continue
		}

		// Build RRuleSet for expansion
		ruleSet, err := buildRRuleSet(rruleProp, start, ev, loc)
		if err != nil {
			// Can't parse rule, keep original occurrence
			out = append(out, makeExpandedEvent(ev, start, end, allDay, cancelled))
			continue
		}

		uid := propValue(ev, ics.ComponentPropertyUniqueId)
		occurrences := ruleSet.Between(start.Add(-time.Second), horizon, true)

		if len(occurrences) == 0 {
			// All occurrences beyond horizon; keep original so far-future events aren't lost
			out = append(out, makeExpandedEvent(ev, start, end, allDay, cancelled))
			continue
		}

		for _, occ := range occurrences {
			key := overrideKey{uid, occ.Format("2006-01-02")}
			if ov, ok := overrides[key]; ok {
				ovStart, ovAllDay, err := getEventStart(ov, loc)
				if err != nil {
					continue
				}
				ovEnd, _, _ := getEventEnd(ov, loc)
				out = append(out, makeExpandedEvent(ov, ovStart, ovEnd, ovAllDay, isCancelled(ov)))
			} else {
				occEnd := occ.Add(duration)
				out = append(out, makeExpandedEvent(ev, occ, occEnd, allDay, cancelled))
			}
		}
	}

	return out, nil
}

func makeExpandedEvent(ev *ics.VEvent, start, end time.Time, allDay, cancelled bool) ExpandedEvent {
	return ExpandedEvent{
		Summary:     propValue(ev, ics.ComponentPropertySummary),
		Location:    propValue(ev, ics.ComponentPropertyLocation),
		Description: propValue(ev, ics.ComponentPropertyDescription),
		Start:       start,
		End:         end,
		AllDay:      allDay,
		Cancelled:   cancelled,
	}
}

func propValue(ev *ics.VEvent, prop ics.ComponentProperty) string {
	p := ev.GetProperty(prop)
	if p == nil {
		return ""
	}
	return p.Value
}

func isCancelled(ev *ics.VEvent) bool {
	status := strings.ToUpper(propValue(ev, ics.ComponentPropertyStatus))
	if status == "CANCELLED" {
		return true
	}
	summary := strings.ToLower(propValue(ev, ics.ComponentPropertySummary))
	return strings.Contains(summary, "cancelled") || strings.Contains(summary, "canceled")
}

func getEventStart(ev *ics.VEvent, loc *time.Location) (time.Time, bool, error) {
	prop := ev.GetProperty(ics.ComponentPropertyDtStart)
	if prop == nil {
		return time.Time{}, false, fmt.Errorf("no DTSTART")
	}
	return parseRawTimestamp(prop.Value, loc)
}

func getEventEnd(ev *ics.VEvent, loc *time.Location) (time.Time, bool, error) {
	prop := ev.GetProperty(ics.ComponentPropertyDtEnd)
	if prop == nil {
		return time.Time{}, false, fmt.Errorf("no DTEND")
	}
	return parseRawTimestamp(prop.Value, loc)
}

// parseRawTimestamp parses ICS timestamp formats: 20060102T150405Z, 20060102T150405, 20060102.
func parseRawTimestamp(ts string, loc *time.Location) (time.Time, bool, error) {
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

func buildRRuleSet(rruleStr string, dtstart time.Time, ev *ics.VEvent, loc *time.Location) (*rrule.Set, error) {
	rOption, err := rrule.StrToROption(fmt.Sprintf("RRULE:%s", rruleStr))
	if err != nil {
		return nil, err
	}
	rOption.Dtstart = dtstart

	rule, err := rrule.NewRRule(*rOption)
	if err != nil {
		return nil, err
	}

	set := &rrule.Set{}
	set.RRule(rule)

	// Add EXDATEs
	for _, prop := range ev.Properties {
		if prop.IANAToken == string(ics.ComponentPropertyExdate) {
			if t, _, err := parseRawTimestamp(prop.Value, loc); err == nil {
				set.ExDate(t)
			}
		}
	}

	return set, nil
}

// formatTimeRange returns a formatted time string like "12:00 - 13:00" for non-all-day events.
func formatTimeRange(ev ExpandedEvent) *string {
	if ev.AllDay {
		return nil
	}
	t := ev.Start.Format("15:04")
	if !ev.End.IsZero() && !ev.AllDay {
		r := fmt.Sprintf("%s - %s", t, ev.End.Format("15:04"))
		return &r
	}
	return &t
}

// strPtr returns a pointer to s, or nil if s is empty.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
