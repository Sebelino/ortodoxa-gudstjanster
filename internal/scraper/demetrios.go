package scraper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/srpska"
	"ortodoxa-gudstjanster/internal/store"
	"ortodoxa-gudstjanster/internal/vision"
)

const (
	demetriosSourceName = "De Helige Demetrios & Nestor grekisk-ortodoxa församling"
	demetriosParishSlug = "de-helige-demetrios-nestor"
	demetriosURL        = "https://grekiskortodoxakyrkan.se/kyrkoschema/"
	demetriosLocation   = "Björkrisvägen 18, 702 34 Örebro"
)

// DemetriosScraper scrapes the schedule for De Helige Demetrios & Nestor in Örebro.
//
// The parish publishes one <p> line per calendar day (or week) on a plain
// WordPress page, in free text whose shape changes from week to week:
//
//	Söndag 6:e September: Liturgi 10:00 och Orhros kl 09:00 i Brickebacken.
//	Söndag 18 okt kl 10:00 Gudomlig Liturgi ... med start 09:00(...) Orthros.
//	Söndag 25 ok: Ingen Liturgi på Söndag, då jag är i Stockholm
//
// Regexes broke on every drift (abbreviated or misspelled months, missing
// colons, lead-in words), and an uncached AI parse split multi-service lines
// inconsistently between runs. So each schedule-looking line is read once by
// the AI line parser and the validated result is cached in the store keyed by
// the line's text: an unchanged line always yields identical services, and only
// new or edited lines cost an API call. Each service's wording is kept verbatim
// as the Beskrivning.
//
// A line that looks like a schedule entry but can't be read (API failure, or
// output that fails validation against the line) is flagged in FetchNotes, and
// it is a hard error if nothing at all could be read, so a format change is
// loud rather than silent.
type DemetriosScraper struct {
	NoteCollector
	store  store.Store
	parser demetriosLineParser
	now    func() time.Time
}

// demetriosLineParser reads one free-text schedule line; *vision.Client implements it.
type demetriosLineParser interface {
	ParseScheduleLine(ctx context.Context, line string) (*vision.LineSchedule, error)
}

// demetriosCacheVersion must be bumped when the prompt or validation changes in
// a way that should re-read already cached lines.
const demetriosCacheVersion = "demetrios-lines/v1/"

// NewDemetriosScraper creates a new scraper for De Helige Demetrios & Nestor.
func NewDemetriosScraper(s store.Store, v *vision.Client) *DemetriosScraper {
	return &DemetriosScraper{store: s, parser: v, now: time.Now}
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

	now := s.now().In(stockholm)
	var services []model.ChurchService
	var unparsed []string
	lines, cacheHits := 0, 0

	doc.Find(".custom-posts article p").Each(func(_ int, p *goquery.Selection) {
		text := strings.Join(strings.Fields(p.Text()), " ")
		if text == "" || !looksLikeDemetriosSchedule(text) {
			return
		}
		lines++

		parsed, hit, err := s.readLine(ctx, text)
		if err != nil {
			log.Printf("Demetrios: failed to read line %q: %v", text, err)
			unparsed = append(unparsed, text)
			return
		}
		if hit {
			cacheHits++
		}

		before, cancelled := len(services), false
		for _, day := range parsed.Days {
			if len(day.Services) == 0 {
				cancelled = true
				s.note("no services announced for %d/%d: %q", day.Day, day.Month, text)
				continue
			}
			date, weekday := demetriosResolveDate(day.Day, day.Month, now)
			for _, svc := range day.Services {
				services = append(services, demetriosService(date, weekday, svc))
			}
		}
		if len(services) == before && !cancelled {
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

	s.note("read %d schedule-looking lines (%d cached) into %d services (%d unparsed)", lines, cacheHits, len(services), len(unparsed))
	return services, nil
}

// readLine returns the validated reading of a line, from the store if the exact
// line was read before, otherwise from the AI parser (and then cached). Results
// that fail validation are neither returned nor cached.
func (s *DemetriosScraper) readLine(ctx context.Context, line string) (parsed *vision.LineSchedule, cacheHit bool, err error) {
	sum := sha256.Sum256([]byte(line))
	key := demetriosCacheVersion + hex.EncodeToString(sum[:])

	var cached vision.LineSchedule
	if s.store != nil && s.store.GetJSON(key, &cached) {
		return &cached, true, nil
	}

	result, err := s.parser.ParseScheduleLine(ctx, line)
	if err != nil {
		return nil, false, err
	}
	if err := validateDemetriosLine(line, result); err != nil {
		return nil, false, fmt.Errorf("invalid parse: %w", err)
	}
	if s.store != nil {
		if err := s.store.SetJSON(key, result); err != nil {
			log.Printf("warning: failed to cache demetrios line: %v", err)
		}
	}
	return result, false, nil
}

var (
	// "kl 09:00", "kl:11", "kl. 9.00" — an explicit clock reference.
	demetriosKlTimeRE = regexp.MustCompile(`(?i)\bkl[\s.:]*(\d{1,2})(?:[.:](\d{2}))?`)
	// A bare "HH:MM" / "HH.MM" not glued to a word or an email local-part.
	demetriosBareTimeRE = regexp.MustCompile(`(^|[^\w@])(\d{1,2})[.:](\d{2})([^\w]|$)`)
	// A line worth reading: mentions a weekday or a full month name, or contains a
	// clock time. Saint headings ("5 sep: Helige Profeten ...") and quotes don't.
	demetriosCandidateRE = regexp.MustCompile(`(?i)\b(måndag|tisdag|onsdag|torsdag|fredag|lördag|söndag|januari|februari|mars|april|maj|juni|juli|augusti|september|oktober|november|december)\b`)
	demetriosClockRE     = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
)

// looksLikeDemetriosSchedule reports whether a line is worth sending to the line
// parser, i.e. looks like it was meant to be a schedule entry.
func looksLikeDemetriosSchedule(text string) bool {
	return demetriosCandidateRE.MatchString(text) || demetriosTime(text) != ""
}

// demetriosMonthStems are the prefixes a line must contain (case-insensitively)
// for a parsed month to be believable. "ok" covers the parish's "okt"/"ok".
var demetriosMonthStems = map[int]string{
	1: "jan", 2: "feb", 3: "mar", 4: "apr", 5: "maj", 6: "jun",
	7: "jul", 8: "aug", 9: "sep", 10: "ok", 11: "nov", 12: "dec",
}

// validateDemetriosLine checks the AI reading against the line so a bad parse is
// rejected instead of stored: every date's day number and month must appear in
// the line, every time must be a valid HH:MM whose hour appears in the line, and
// every name must be a verbatim fragment of the line.
func validateDemetriosLine(line string, ls *vision.LineSchedule) error {
	lower := strings.ToLower(line)
	for _, day := range ls.Days {
		stem, ok := demetriosMonthStems[day.Month]
		if day.Day < 1 || day.Day > 31 || !ok {
			return fmt.Errorf("bad date %d/%d", day.Day, day.Month)
		}
		if !regexp.MustCompile(`(^|\D)0?` + fmt.Sprint(day.Day) + `(\D|$)`).MatchString(line) {
			return fmt.Errorf("day %d not in line", day.Day)
		}
		if !strings.Contains(lower, stem) {
			return fmt.Errorf("month %d (%q) not in line", day.Month, stem)
		}
		for _, svc := range day.Services {
			if !demetriosClockRE.MatchString(svc.Time) {
				return fmt.Errorf("bad time %q", svc.Time)
			}
			hour := strings.TrimPrefix(svc.Time[:2], "0")
			if hour == "" {
				hour = "0"
			}
			if !regexp.MustCompile(`(^|\D)0?` + hour + `(\D|$)`).MatchString(line) {
				return fmt.Errorf("time %s not in line", svc.Time)
			}
			name := strings.TrimSpace(svc.Name)
			if name == "" || !strings.Contains(lower, strings.ToLower(name)) {
				return fmt.Errorf("name %q is not a fragment of the line", svc.Name)
			}
		}
	}
	return nil
}

// demetriosResolveDate places a day/month in the [-3, +9] month window around
// now (the page never states a year) and returns the ISO date plus the Swedish
// weekday computed from it.
func demetriosResolveDate(day, month int, now time.Time) (date, weekday string) {
	d := time.Date(now.Year(), time.Month(month), day, 0, 0, 0, 0, now.Location())
	if d.Before(now.AddDate(0, -3, 0)) {
		d = d.AddDate(1, 0, 0)
	} else if d.After(now.AddDate(0, 9, 0)) {
		d = d.AddDate(-1, 0, 0)
	}
	return d.Format("2006-01-02"), srpska.WeekdayToSwedish(d.Weekday())
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

// demetriosService turns one parsed service into a ChurchService. The name is
// kept verbatim as the Beskrivning; an Orthros gets the parish's own term as its
// short title (they consistently misspell it "Orhros"), others are left for the
// normal AI title generation.
func demetriosService(date, weekday string, svc vision.LineService) model.ChurchService {
	title := ""
	if svc.IsOrthros {
		title = "Orthros"
	}
	location := demetriosLocation
	t := svc.Time
	return model.ChurchService{
		ParishSlug:  demetriosParishSlug,
		Source:      demetriosSourceName,
		SourceURL:   demetriosURL,
		Date:        date,
		DayOfWeek:   weekday,
		ServiceName: strings.Trim(strings.TrimSpace(svc.Name), ". "),
		Title:       title,
		Location:    &location,
		Time:        &t,
	}
}

func atoiDefault(s string, def int) int {
	n := def
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n); err != nil {
		return def
	}
	return n
}
