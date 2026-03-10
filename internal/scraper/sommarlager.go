package scraper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/store"
	"ortodoxa-gudstjanster/internal/vision"
)

const (
	sommarlagerSourceName = "Ortodoxt sommarlager"
	sommarlagerURL        = "https://ortodoxtsommarlager.se"
	sommarlagerRegURL     = "https://ortodoxtsommarlager.se/?page_id=60"
)

// SommarlagerScraper scrapes the Orthodox summer camp website.
type SommarlagerScraper struct {
	store  store.Store
	vision *vision.Client
}

// NewSommarlagerScraper creates a new scraper for the Orthodox summer camp.
func NewSommarlagerScraper(s store.Store, v *vision.Client) *SommarlagerScraper {
	return &SommarlagerScraper{
		store:  s,
		vision: v,
	}
}

func (s *SommarlagerScraper) Name() string {
	return sommarlagerSourceName
}

func (s *SommarlagerScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	// Fetch main page and registration page
	mainText, err := fetchPageText(ctx, sommarlagerURL)
	if err != nil {
		return nil, fmt.Errorf("fetching main page: %w", err)
	}

	regText, err := fetchPageText(ctx, sommarlagerRegURL)
	if err != nil {
		log.Printf("sommarlager: failed to fetch registration page: %v", err)
		regText = ""
	}

	combined := mainText
	if regText != "" {
		combined += "\n\n--- Registration page ---\n\n" + regText
	}

	// Check cache by content hash
	hash := sha256.Sum256([]byte(combined))
	checksum := hex.EncodeToString(hash[:])
	cacheKey := "sommarlager/v1/" + checksum

	var events []vision.CampEvent
	if s.store.GetJSON(cacheKey, &events) {
		log.Printf("sommarlager: using cached result (%d events)", len(events))
		return s.eventsToServices(events), nil
	}

	// Use AI to extract events
	events, err = s.vision.ExtractCampEvents(ctx, combined)
	if err != nil {
		return nil, fmt.Errorf("extracting camp events: %w", err)
	}

	// Cache result
	if err := s.store.SetJSON(cacheKey, events); err != nil {
		log.Printf("sommarlager: failed to cache result: %v", err)
	}

	log.Printf("sommarlager: extracted %d events", len(events))
	return s.eventsToServices(events), nil
}

// fetchPageText fetches an HTML page and extracts its visible text content.
func fetchPageText(ctx context.Context, url string) (string, error) {
	doc, err := fetchDocument(ctx, url)
	if err != nil {
		return "", err
	}

	// Remove script and style elements
	doc.Find("script, style, noscript").Remove()

	// Extract text
	var parts []string
	doc.Find("body").Each(func(_ int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if text != "" {
			parts = append(parts, text)
		}
	})

	return strings.Join(parts, "\n"), nil
}

func (s *SommarlagerScraper) eventsToServices(events []vision.CampEvent) []model.ChurchService {
	var services []model.ChurchService

	for _, event := range events {
		var notesPtr *string
		if event.Notes != "" {
			notes := event.Notes
			notesPtr = &notes
		}

		services = append(services, model.ChurchService{
			Parish:    sommarlagerSourceName,
			Source:    sommarlagerSourceName,
			SourceURL: sommarlagerURL,
			Date:      event.Date,
			DayOfWeek: event.DayOfWeek,
			ServiceName: event.ServiceName,
			Notes:     notesPtr,
		})
	}

	return services
}
