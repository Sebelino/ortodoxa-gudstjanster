package scraper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"regexp"
	"strings"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/store"
	"ortodoxa-gudstjanster/internal/vision"
)

// ExtractRyskaScheduleText fetches the Ryska website and extracts the schedule text.
// This is exported so it can be used by the extract-text tool for testing.
func ExtractRyskaScheduleText(ctx context.Context) (string, error) {
	bodyBytes, err := fetchURL(ctx, ryskaURL)
	if err != nil {
		return "", err
	}

	return ExtractRyskaScheduleTextFromHTML(string(bodyBytes)), nil
}

// ExtractRyskaScheduleTextFromHTML extracts schedule text from raw HTML.
func ExtractRyskaScheduleTextFromHTML(htmlContent string) string {
	// Strip HTML tags and decode entities
	content := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(htmlContent, " ")
	content = html.UnescapeString(content)
	content = regexp.MustCompile(`\s+`).ReplaceAllString(content, " ")

	// Extract just the schedule section (from "Januari" to "bottom of page" or similar)
	schedulePattern := regexp.MustCompile(`(?i)(Januari\s.+?)(?:bottom of page|KRISTI FÖRKLARINGS|$)`)
	if match := schedulePattern.FindStringSubmatch(content); len(match) > 1 {
		content = match[1]
	}

	// Add newlines for better structure
	content = regexp.MustCompile(`\s+(Januari|Februari|Mars|April|Maj|Juni|Juli|Augusti|September|Oktober|November|December)\s`).ReplaceAllString(content, "\n\n$1\n")
	content = regexp.MustCompile(`\s+(\d{1,2}\s+(?:Söndag|Måndag|Tisdag|Onsdag|Torsdag|Fredag|Lördag))`).ReplaceAllString(content, "\n$1")

	return strings.TrimSpace(content)
}

const (
	ryskaSourceName = "Kristi Förklarings Ortodoxa Församling"
	ryskaURL        = "https://www.ryskaortodoxakyrkan.se/gudstjänst"
	ryskaLocation   = "Stockholm, Birger Jarlsgatan 98"
	ryskaLanguage   = "Kyrkoslaviska, svenska"
)

// RyskaScraper scrapes the Russian Orthodox Church schedule.
type RyskaScraper struct {
	store  store.Store
	vision *vision.Client
}

// NewRyskaScraper creates a new scraper for the Russian Orthodox Church.
func NewRyskaScraper(s store.Store, v *vision.Client) *RyskaScraper {
	return &RyskaScraper{
		store:  s,
		vision: v,
	}
}

func (s *RyskaScraper) Name() string {
	return ryskaSourceName
}

func (s *RyskaScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	content, err := ExtractRyskaScheduleText(ctx)
	if err != nil {
		return nil, err
	}

	// Compute checksum for caching
	hash := sha256.Sum256([]byte(content))
	checksum := hex.EncodeToString(hash[:])
	// Check store for cached result
	var entries []vision.ScheduleEntry
	if s.store.GetJSON(checksum, &entries) {
		return s.entriesToServices(entries), nil
	}

	// Use OpenAI to extract schedule from text
	entries, err = s.vision.ExtractScheduleFromText(ctx, content)
	if err != nil {
		return nil, fmt.Errorf("extracting schedule: %w", err)
	}

	// Cache result
	if err := s.store.SetJSON(checksum, entries); err != nil {
		// Log but don't fail
		fmt.Printf("warning: failed to cache ryska schedule: %v\n", err)
	}

	return s.entriesToServices(entries), nil
}

func (s *RyskaScraper) entriesToServices(entries []vision.ScheduleEntry) []model.ChurchService {
	var services []model.ChurchService
	location := ryskaLocation
	lang := ryskaLanguage

	for _, entry := range entries {
		var timePtr *string
		if entry.Time != "" {
			timePtr = &entry.Time
		}

		var occasionPtr *string
		if entry.Occasion != "" {
			occasionPtr = &entry.Occasion
		}

		services = append(services, model.ChurchService{
			Source:      ryskaSourceName,
			SourceURL:   ryskaURL,
			Date:        entry.Date,
			DayOfWeek:   entry.DayOfWeek,
			ServiceName: entry.ServiceName,
			Location:    &location,
			Time:        timePtr,
			Occasion:    occasionPtr,
			Notes:       nil,
			Language:    &lang,
		})
	}

	return services
}
