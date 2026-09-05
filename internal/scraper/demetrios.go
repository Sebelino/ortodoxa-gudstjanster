package scraper

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/srpska"
)

const (
	demetriosSourceName = "De Helige Demetrios & Nestor grekisk-ortodoxa församling"
	demetriosParishSlug = "de-helige-demetrios-nestor"
	demetriosURL        = "https://grekiskortodoxakyrkan.se/kyrkoschema/"
	demetriosLocation   = "Björkrisvägen 18, 702 34 Örebro"
)

// DemetriosScraper scrapes the schedule for De Helige Demetrios & Nestor in Örebro.
//
// The parish publishes one <p> line per calendar day on a plain WordPress page,
// each in the form "Weekday Nth Month: <service prose>", e.g.
//
//	Söndag 6:e September: Liturgi 10:00 och Orhros kl 09:00 i Brickebacken.
//	Lördag 5:e September: Bikt vid kl:11 meddela/boka på gerav23@gmail.com innan
//
// It's parsed deterministically: the "Weekday Nth Month:" prefix gives the date,
// the clause is split into per-service segments (only where every segment carries
// its own time), and each segment's wording is kept verbatim as the Beskrivning.
// An earlier version handed the text to the AI text parser, but that split
// multi-service lines inconsistently between runs.
//
// Because a hand-edited parish page can drift from that shape, Fetch flags any
// <p> line that looks schedule-ish (a month name, weekday, or clock time) but
// yields no service — surfaced in FetchNotes, and a hard error if it means
// nothing at all parsed — so a format change is loud rather than silent.
type DemetriosScraper struct {
	NoteCollector
}

// NewDemetriosScraper creates a new scraper for De Helige Demetrios & Nestor.
func NewDemetriosScraper() *DemetriosScraper {
	return &DemetriosScraper{}
}

func (s *DemetriosScraper) Name() string {
	return demetriosSourceName
}

func (s *DemetriosScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	s.resetNotes()

	doc, err := fetchDocument(ctx, demetriosURL)
	if err != nil {
		return nil, err
	}

	now := time.Now().In(stockholm)
	var services []model.ChurchService
	var unparsed []string
	lineCount := 0

	doc.Find(".custom-posts article p").Each(func(_ int, p *goquery.Selection) {
		text := strings.TrimSpace(p.Text())
		if text == "" {
			return
		}

		date, weekday, clause, ok := parseDemetriosLine(text, now)
		if !ok {
			if looksLikeDemetriosSchedule(text) {
				unparsed = append(unparsed, text)
			}
			return
		}
		lineCount++

		before := len(services)
		for _, seg := range splitDemetriosClause(clause) {
			if svc, ok := demetriosSegmentToService(date, weekday, seg); ok {
				services = append(services, svc)
			}
		}
		if len(services) == before {
			unparsed = append(unparsed, text)
		}
	})

	for _, u := range unparsed {
		log.Printf("Demetrios: failed to parse schedule-looking line: %q", u)
		s.note("failed to parse schedule-looking line: %q", u)
	}

	if len(services) == 0 {
		if len(unparsed) > 0 {
			return nil, fmt.Errorf("%d schedule-looking lines on %s but none could be parsed (page format may have changed); first: %q",
				len(unparsed), demetriosURL, unparsed[0])
		}
		return nil, fmt.Errorf("no schedule content found on %s", demetriosURL)
	}

	s.note("parsed %d dated lines into %d services (%d unparsed)", lineCount, len(services), len(unparsed))
	return services, nil
}

var (
	demetriosLineRE  = regexp.MustCompile(`(?i)^\s*(måndag|tisdag|onsdag|torsdag|fredag|lördag|söndag)\s+(\d{1,2})(?::e|:a)?\s+(januari|februari|mars|april|maj|juni|juli|augusti|september|oktober|november|december)\b\s*:?\s*(.*)$`)
	demetriosSplitRE = regexp.MustCompile(`(?i)\s+och\s+|,\s+`)
	// "kl 09:00", "kl:11", "kl. 9.00" — an explicit clock reference.
	demetriosKlTimeRE = regexp.MustCompile(`(?i)\bkl[\s.:]*(\d{1,2})(?:[.:](\d{2}))?`)
	// A bare "HH:MM" / "HH.MM" not glued to a word or an email local-part.
	demetriosBareTimeRE = regexp.MustCompile(`(^|[^\w@])(\d{1,2})[.:](\d{2})([^\w]|$)`)
	// A line worth flagging if it fails to parse: mentions a weekday or a full
	// month name, or contains a clock time.
	demetriosCandidateRE = regexp.MustCompile(`(?i)\b(måndag|tisdag|onsdag|torsdag|fredag|lördag|söndag|januari|februari|mars|april|maj|juni|juli|augusti|september|oktober|november|december)\b`)
)

var demetriosMonths = map[string]time.Month{
	"januari": time.January, "februari": time.February, "mars": time.March,
	"april": time.April, "maj": time.May, "juni": time.June, "juli": time.July,
	"augusti": time.August, "september": time.September, "oktober": time.October,
	"november": time.November, "december": time.December,
}

// looksLikeDemetriosSchedule reports whether a line that failed to parse still
// looks like it was meant to be a schedule entry (so drift gets flagged rather
// than silently skipped alongside genuinely unrelated text like the monthly quote).
func looksLikeDemetriosSchedule(text string) bool {
	return demetriosCandidateRE.MatchString(text) || demetriosTime(text) != ""
}

// parseDemetriosLine splits a "Lördag 5:e September: <clause>" line into its
// calendar date, the Swedish weekday computed from that date, and the clause.
// The year is inferred by placing the date in a [-3, +9] month window around now
// (the page never states a year). Lines that don't match the shape are skipped.
func parseDemetriosLine(text string, now time.Time) (date, weekday, clause string, ok bool) {
	m := demetriosLineRE.FindStringSubmatch(text)
	if m == nil {
		return "", "", "", false
	}
	day := atoiDefault(m[2], 0)
	month, monthOK := demetriosMonths[strings.ToLower(m[3])]
	if day < 1 || day > 31 || !monthOK {
		return "", "", "", false
	}
	clause = strings.TrimSpace(m[4])
	if clause == "" {
		return "", "", "", false
	}

	year := now.Year()
	d := time.Date(year, month, day, 0, 0, 0, 0, now.Location())
	if d.Before(now.AddDate(0, -3, 0)) {
		year++
		d = d.AddDate(1, 0, 0)
	} else if d.After(now.AddDate(0, 9, 0)) {
		year--
		d = d.AddDate(-1, 0, 0)
	}
	return fmt.Sprintf("%d-%02d-%02d", year, int(month), day), srpska.WeekdayToSwedish(d.Weekday()), clause, true
}

// splitDemetriosClause breaks a clause into per-service segments, but only when
// every resulting segment carries its own clock time. "Liturgi 10:00 och Orhros
// kl 09:00 i Brickebacken" splits in two; "17:00 Esperinos och Ikon&Koboskini
// tillverkning samt bikt" (second half has no time) stays whole.
func splitDemetriosClause(clause string) []string {
	parts := demetriosSplitRE.Split(clause, -1)
	if len(parts) < 2 {
		return []string{clause}
	}
	for _, p := range parts {
		if demetriosTime(p) == "" {
			return []string{clause}
		}
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// demetriosTime returns the first clock time in a segment as "HH:MM", or "".
func demetriosTime(seg string) string {
	if m := demetriosKlTimeRE.FindStringSubmatch(seg); m != nil {
		h := atoiDefault(m[1], -1)
		min := 0
		if m[2] != "" {
			min = atoiDefault(m[2], 0)
		}
		if h >= 0 && h < 24 && min < 60 {
			return fmt.Sprintf("%02d:%02d", h, min)
		}
	}
	if m := demetriosBareTimeRE.FindStringSubmatch(seg); m != nil {
		h, min := atoiDefault(m[2], -1), atoiDefault(m[3], -1)
		if h >= 0 && h < 24 && min >= 0 && min < 60 {
			return fmt.Sprintf("%02d:%02d", h, min)
		}
	}
	return ""
}

// demetriosSegmentToService turns one service segment into a ChurchService. The
// segment text is kept verbatim as the Beskrivning (only its own clock time is
// also pulled out into the Time field); a segment with no time is not a service.
func demetriosSegmentToService(date, weekday, seg string) (model.ChurchService, bool) {
	seg = strings.Trim(strings.TrimSpace(seg), ". ")
	if seg == "" {
		return model.ChurchService{}, false
	}
	t := demetriosTime(seg)
	if t == "" {
		return model.ChurchService{}, false
	}

	location := demetriosLocation
	return model.ChurchService{
		ParishSlug:  demetriosParishSlug,
		Source:      demetriosSourceName,
		SourceURL:   demetriosURL,
		Date:        date,
		DayOfWeek:   weekday,
		ServiceName: seg,
		Title:       demetriosTitleFor(seg),
		Location:    &location,
		Time:        &t,
	}, true
}

// demetriosTitleOverrides maps the parish's own service wording to the short
// title to display. The Beskrivning keeps their wording (and spelling) verbatim;
// only the standardized short title is adjusted — here to their own term
// "Orthros" (which they consistently misspell "Orhros") rather than the generic
// "Morgongudstjänst", since they write in Swedish and use their own vocabulary.
var demetriosTitleOverrides = []struct {
	re    *regexp.Regexp
	title string
}{
	{regexp.MustCompile(`(?i)^\s*ort?hros\b`), "Orthros"},
}

// demetriosTitleFor returns an explicit short title for a service name, or ""
// to let the normal AI title generation handle it.
func demetriosTitleFor(serviceName string) string {
	for _, o := range demetriosTitleOverrides {
		if o.re.MatchString(serviceName) {
			return o.title
		}
	}
	return ""
}

func atoiDefault(s string, def int) int {
	n := def
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n); err != nil {
		return def
	}
	return n
}
