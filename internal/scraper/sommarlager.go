package scraper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/store"
	"ortodoxa-gudstjanster/internal/vision"
)

const (
	sommarlagerSourceName = "Ortodoxt sommarläger"
	sommarlagerURL        = "https://ortodoxtsommarlager.se"
)

// SommarlagerScraper scrapes the Orthodox summer camp website.
type SommarlagerScraper struct {
	NoteCollector
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
	s.resetNotes()

	// Fetch main page
	mainDoc, err := fetchDocument(ctx, sommarlagerURL)
	if err != nil {
		return nil, fmt.Errorf("fetching main page: %w", err)
	}

	mainDoc.Find("script, style, noscript").Remove()
	mainText := strings.TrimSpace(mainDoc.Find("body").Text())

	// Discover registration page link from main page
	regText := ""
	regURL := findRegistrationLink(mainDoc)
	if regURL != "" {
		log.Printf("sommarlager: found registration link: %s", regURL)
		s.note("registration link found: %s", regURL)
		text, err := fetchPageText(ctx, regURL)
		if err != nil {
			log.Printf("sommarlager: failed to fetch registration page: %v", err)
			s.note("registration page fetch failed: %v", err)
		} else {
			regText = text
		}
	} else {
		log.Printf("sommarlager: no registration link found on main page")
		s.note("no registration link found on main page")
	}

	combined := mainText
	if regText != "" {
		combined += "\n\n--- Registration page ---\n\n" + regText
	}

	// Check cache by content hash
	hash := sha256.Sum256([]byte(combined))
	checksum := hex.EncodeToString(hash[:])
	cacheKey := "sommarlager/v5/" + checksum

	var events []vision.CampEvent
	notice := extractSommarlagerNotice(combined)
	log.Printf("sommarlager: notice=%q", notice)
	if s.store.GetJSON(cacheKey, &events) {
		log.Printf("sommarlager: using cached result (%d events)", len(events))
		s.note("cache hit: %d events", len(events))
		return s.eventsToServices(events, notice), nil
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
	s.note("AI extraction: %d events", len(events))
	return s.eventsToServices(events, notice), nil
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

// findRegistrationLink searches the page for a link whose text suggests
// registration (e.g., "Anmälan", "Anmälning", "Registrera").
func findRegistrationLink(doc *goquery.Document) string {
	keywords := []string{"anmäl", "registrer"}
	var found string
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		if found != "" {
			return
		}
		text := strings.ToLower(strings.TrimSpace(a.Text()))
		href, exists := a.Attr("href")
		if !exists || href == "" {
			return
		}
		for _, kw := range keywords {
			if strings.Contains(text, kw) {
				// Make absolute if relative
				if strings.HasPrefix(href, "/") || strings.HasPrefix(href, "?") {
					href = sommarlagerURL + href
				}
				found = href
				return
			}
		}
	})
	return found
}

// extractSommarlagerNotice scans page text for a prominent closed-registration
// or OBSERVERA notice and returns it as a single string, or "" if not found.
func extractSommarlagerNotice(text string) string {
	lower := strings.ToLower(text)
	keywords := []string{"observera", "är nu stängd", "är stängd", "återbud"}
	for _, kw := range keywords {
		idx := strings.Index(lower, kw)
		if idx == -1 {
			continue
		}
		// Walk back to the start of the line
		start := strings.LastIndex(text[:idx], "\n")
		if start == -1 {
			start = 0
		} else {
			start++
		}
		// Take text up to the first paragraph break ( \n  is WordPress's empty paragraph),
		// or up to 300 chars, trimmed to a sentence boundary.
		chunk := strings.TrimSpace(text[start:])
		if i := strings.Index(chunk, " \n "); i != -1 {
			chunk = chunk[:i]
			if j := strings.LastIndexAny(chunk, ".!?"); j != -1 {
				chunk = chunk[:j+1]
			}
		} else if len(chunk) > 300 {
			chunk = chunk[:300]
			if j := strings.LastIndexAny(chunk, ".!?"); j != -1 {
				chunk = chunk[:j+1]
			}
		}
		return strings.TrimSpace(chunk)
	}
	return ""
}

func (s *SommarlagerScraper) eventsToServices(events []vision.CampEvent, notice string) []model.ChurchService {
	var services []model.ChurchService
	lang := "Svenska"

	for _, event := range events {
		notes := event.Notes
		if notice != "" {
			if notes != "" {
				notes = notice + " " + notes
			} else {
				notes = notice
			}
		}
		var notesPtr *string
		if notes != "" {
			notesPtr = &notes
		}

		var title string
		if strings.HasPrefix(event.ServiceName, "Sista anmälningsdag") {
			title = "Sista anmälningsdag: Sommarläger"
		}

		svc := model.ChurchService{
			Source:         sommarlagerSourceName,
			SourceURL:      sommarlagerURL,
			Date:           event.Date,
			DayOfWeek:      event.DayOfWeek,
			ServiceName:    event.ServiceName,
			Title:          title,
			Notes:          notesPtr,
			ParishLanguage: &lang,
		}

		// For multi-day events, set start time to 00:00 on start date
		// and end time to 23:59 on end date
		if event.EndDate != "" {
			startDate, err1 := time.Parse("2006-01-02", event.Date)
			endDate, err2 := time.Parse("2006-01-02", event.EndDate)
			if err1 == nil && err2 == nil {
				start := time.Date(startDate.Year(), startDate.Month(), startDate.Day(), 0, 0, 0, 0, stockholm)
				end := time.Date(endDate.Year(), endDate.Month(), endDate.Day(), 23, 59, 0, 0, stockholm)
				svc.StartTime = &start
				svc.EndTime = &end
			}
		}

		services = append(services, svc)
	}

	return services
}
