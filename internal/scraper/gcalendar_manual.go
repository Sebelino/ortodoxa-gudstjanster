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

var htmlBrRE = regexp.MustCompile(`(?i)<br\s*/?>`)
var htmlTagRE = regexp.MustCompile(`<[^>]+>`)

const (
	gcalendarManualSourceName = "Manuella händelser (Google Kalender)"
	gcalendarManualURL        = "https://calendar.google.com/calendar/ical/baa3943fce1521aabda755b4eb192b1cc8d7579294eab99a8eb89f024ab6b218@group.calendar.google.com/public/basic.ics"
	gcalendarManualSourcePage = "https://calendar.google.com/calendar/embed?src=baa3943fce1521aabda755b4eb192b1cc8d7579294eab99a8eb89f024ab6b218%40group.calendar.google.com&ctz=Europe%2FStockholm"
)

// Regex patterns for structured fields in the DESCRIPTION body.
// Each event's description is expected to contain lines like:
//
//	Församling: St. Georgios Cathedral
//	Språk: Engelska
//	Källa: Whatsapp-grupp Ortodoxi Sverige
//	Beskrivning: Undervisning för katekumener.
var (
	gcalManualParishRE   = regexp.MustCompile(`(?im)^\s*F[öo]rsamling\s*:\s*(.+?)\s*$`)
	gcalManualLanguageRE = regexp.MustCompile(`(?im)^\s*Spr[åa]k\s*:\s*(.+?)\s*$`)
	gcalManualSourceRE   = regexp.MustCompile(`(?im)^\s*K[äa]lla\s*:\s*(.+?)\s*$`)
	gcalManualDescRE     = regexp.MustCompile(`(?im)^\s*Beskrivning\s*:\s*(.+?)\s*$`)
)

// GCalendarManualScraper fetches events from a user-curated Google Calendar
// where the parish and language are embedded in each event's DESCRIPTION.
type GCalendarManualScraper struct{ NoteCollector }

func NewGCalendarManualScraper() *GCalendarManualScraper {
	return &GCalendarManualScraper{}
}

func (s *GCalendarManualScraper) Name() string {
	return gcalendarManualSourceName
}

func (s *GCalendarManualScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	s.resetNotes()
	data, err := fetchURL(ctx, gcalendarManualURL)
	if err != nil {
		return nil, fmt.Errorf("fetching ICS feed: %w", err)
	}

	stockholm, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		return nil, fmt.Errorf("loading timezone: %w", err)
	}

	events, err := ParseAndExpandICS(string(data), stockholm)
	if err != nil {
		return nil, fmt.Errorf("parsing ICS feed: %w", err)
	}

	var services []model.ChurchService
	skipped := 0
	for _, ev := range events {
		if ev.Cancelled {
			continue
		}

		// Normalize HTML: Google Calendar basic.ics may use <br> instead of \n
		desc := htmlBrRE.ReplaceAllString(ev.Description, "\n")
		desc = htmlTagRE.ReplaceAllString(desc, "")

		parish := firstSubmatch(gcalManualParishRE, desc)
		if parish == "" {
			skipped++
			continue
		}
		language := firstSubmatch(gcalManualLanguageRE, desc)
		source := firstSubmatch(gcalManualSourceRE, desc)
		descField := firstSubmatch(gcalManualDescRE, desc)

		// Use Beskrivning field as notes; fall back to remaining free text
		notesText := descField
		if notesText == "" {
			notesText = stripStructuredFields(desc)
		}

		// Use Källa field as source; fall back to scraper name
		eventSource := gcalendarManualSourceName
		sourceURL := gcalendarManualSourcePage
		if source != "" {
			eventSource = source
			if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
				sourceURL = source
			}
		}

		svc := model.ChurchService{
			Parish:        parish,
			Source:        eventSource,
			SourceURL:     sourceURL,
			Date:          ev.Start.Format("2006-01-02"),
			DayOfWeek:     srpska.WeekdayToSwedish(ev.Start.Weekday()),
			ServiceName:   ev.Summary,
			Location:      strPtr(ev.Location),
			Time:          formatTimeRange(ev),
			Notes:         strPtr(notesText),
			EventLanguage: strPtr(language),
		}
		services = append(services, svc)
	}

	if skipped > 0 {
		s.note("parsed %d events → %d with Församling field, %d skipped (no Församling in description)", len(events), len(services), skipped)
	} else {
		s.note("parsed %d events → %d services", len(events), len(services))
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
