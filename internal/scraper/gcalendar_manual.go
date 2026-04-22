package scraper

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/srpska"
)

const (
	gcalendarManualSourceName = "Manuella händelser (Google Kalender)"
	gcalendarManualURL        = "https://calendar.google.com/calendar/ical/baa3943fce1521aabda755b4eb192b1cc8d7579294eab99a8eb89f024ab6b218@group.calendar.google.com/public/basic.ics"
	gcalendarManualSourcePage = "https://calendar.google.com/calendar/embed?src=baa3943fce1521aabda755b4eb192b1cc8d7579294eab99a8eb89f024ab6b218%40group.calendar.google.com&ctz=Europe%2FStockholm"
)

// Regex patterns for structured fields in the DESCRIPTION body.
// Each event's description is expected to contain lines like:
//   Församling: St. Georgios Cathedral
//   Språk: Engelska
//   Källa: Whatsapp-grupp Ortodoxi Sverige
//   Beskrivning: Undervisning för katekumener.
var (
	gcalManualParishRE   = regexp.MustCompile(`(?im)^\s*F[öo]rsamling\s*:\s*(.+?)\s*$`)
	gcalManualLanguageRE = regexp.MustCompile(`(?im)^\s*Spr[åa]k\s*:\s*(.+?)\s*$`)
	gcalManualSourceRE   = regexp.MustCompile(`(?im)^\s*K[äa]lla\s*:\s*(.+?)\s*$`)
	gcalManualDescRE     = regexp.MustCompile(`(?im)^\s*Beskrivning\s*:\s*(.+?)\s*$`)
)

// GCalendarManualScraper fetches events from a user-curated Google Calendar
// where the parish and language are embedded in each event's DESCRIPTION.
type GCalendarManualScraper struct{}

func NewGCalendarManualScraper() *GCalendarManualScraper {
	return &GCalendarManualScraper{}
}

func (s *GCalendarManualScraper) Name() string {
	return gcalendarManualSourceName
}

func (s *GCalendarManualScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	data, err := fetchURL(ctx, gcalendarManualURL)
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

	// Expand recurring events into individual occurrences
	events = expandRecurringEvents(events, stockholm)

	var services []model.ChurchService
	for _, ev := range events {
		if ev.cancelled {
			continue
		}

		parish := firstSubmatch(gcalManualParishRE, ev.description)
		if parish == "" {
			// Without a parish the event can't be grouped or filtered — skip.
			continue
		}
		language := firstSubmatch(gcalManualLanguageRE, ev.description)
		source := firstSubmatch(gcalManualSourceRE, ev.description)
		desc := firstSubmatch(gcalManualDescRE, ev.description)

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

		// Use Beskrivning field as notes; fall back to remaining free text
		notesText := desc
		if notesText == "" {
			notesText = stripStructuredFields(ev.description)
		}
		var notes *string
		if notesText != "" {
			notes = &notesText
		}

		// Use Källa field as source; fall back to scraper name
		eventSource := gcalendarManualSourceName
		if source != "" {
			eventSource = source
		}

		var location *string
		if ev.location != "" {
			loc := ev.location
			location = &loc
		}

		var eventLang *string
		if language != "" {
			l := language
			eventLang = &l
		}

		svc := model.ChurchService{
			Parish:        parish,
			Source:        eventSource,
			SourceURL:     gcalendarManualSourcePage,
			Date:          date,
			DayOfWeek:     dayOfWeek,
			ServiceName:   ev.summary,
			Location:      location,
			Time:          timeStr,
			Notes:         notes,
			EventLanguage: eventLang,
		}
		services = append(services, svc)
	}

	return services, nil
}

func firstSubmatch(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// gcalManualStructuredFields lists all regexes for structured metadata lines.
var gcalManualStructuredFields = []*regexp.Regexp{
	gcalManualParishRE,
	gcalManualLanguageRE,
	gcalManualSourceRE,
	gcalManualDescRE,
}

// stripStructuredFields removes structured metadata lines from a description,
// returning whatever free-text notes remain.
func stripStructuredFields(desc string) string {
	lines := strings.Split(desc, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		isStructured := false
		for _, re := range gcalManualStructuredFields {
			if re.MatchString(line) {
				isStructured = true
				break
			}
		}
		if !isStructured {
			kept = append(kept, line)
		}
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}
