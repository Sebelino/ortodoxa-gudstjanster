package scraper

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"ortodoxa-gudstjanster/internal/model"
)

const (
	heligaAnnaSourceName = "Heliga Anna av Novgorod"
	heligaAnnaURL        = "https://heligaanna.nu/gudstjanster/"
	heligaAnnaLocation   = "Stockholm, Kyrkvägen 27, Stocksund"
	heligaAnnaLanguage   = "Svenska"
)

// HeligaAnnaScraper scrapes the Heliga Anna av Novgorod schedule.
type HeligaAnnaScraper struct{}

// NewHeligaAnnaScraper creates a new scraper for Heliga Anna av Novgorod.
func NewHeligaAnnaScraper() *HeligaAnnaScraper {
	return &HeligaAnnaScraper{}
}

func (s *HeligaAnnaScraper) Name() string {
	return heligaAnnaSourceName
}

func (s *HeligaAnnaScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	doc, err := fetchDocument(ctx, heligaAnnaURL)
	if err != nil {
		return nil, err
	}

	var services []model.ChurchService
	currentYear := time.Now().Year()

	// Pattern: <strong>Söndag 8/2</strong> kl. 09:00. Liturgi. Optional occasion
	// The text after the service name (after the dot) might be an occasion
	serviceRegex := regexp.MustCompile(`(?i)(måndag|tisdag|onsdag|torsdag|fredag|lördag|söndag)\s+(\d{1,2})/(\d{1,2})`)
	timeRegex := regexp.MustCompile(`kl\.?\s*(\d{1,2})[.:](\d{2})`)

	// Find the Stockholm section - look for h3 with "Stockholm" and get its container
	doc.Find(".elementor-widget-text-editor").Each(func(i int, container *goquery.Selection) {
		html, _ := container.Html()
		if !strings.Contains(html, "<h3>Stockholm</h3>") {
			return
		}

		// Process each list item in this container
		container.Find("li").Each(func(j int, li *goquery.Selection) {
			text := li.Text()

			// Extract day of week and date
			dateMatch := serviceRegex.FindStringSubmatch(text)
			if dateMatch == nil {
				return
			}

			dayOfWeek := capitalize(dateMatch[1])
			day, _ := strconv.Atoi(dateMatch[2])
			month, _ := strconv.Atoi(dateMatch[3])

			// Determine year (if month is before current month, it's next year)
			year := currentYear
			currentMonth := int(time.Now().Month())
			if month < currentMonth {
				year++
			}

			date := fmt.Sprintf("%d-%02d-%02d", year, month, day)

			// Extract time
			var timeStr *string
			if timeMatch := timeRegex.FindStringSubmatch(text); timeMatch != nil {
				t := fmt.Sprintf("%s:%s", timeMatch[1], timeMatch[2])
				timeStr = &t
			}

			// Extract service name and occasion
			// Text after time, before any parenthetical or additional info
			serviceName := "Liturgi"
			var occasion *string

			// Find text after time
			if timeMatch := timeRegex.FindStringIndex(text); timeMatch != nil {
				afterTime := strings.TrimSpace(text[timeMatch[1]:])
				// Remove leading dot if present
				afterTime = strings.TrimPrefix(afterTime, ".")
				afterTime = strings.TrimSpace(afterTime)

				// Split by period - first part is service name, rest might be occasion
				parts := strings.SplitN(afterTime, ".", 2)
				if len(parts) > 0 && parts[0] != "" {
					serviceName = strings.TrimSpace(parts[0])
				}
				if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
					occ := strings.TrimSpace(parts[1])
					occasion = &occ
				}
			}

			location := heligaAnnaLocation
			lang := heligaAnnaLanguage
			services = append(services, model.ChurchService{
				Source:      heligaAnnaSourceName,
				SourceURL:   heligaAnnaURL,
				Date:        date,
				DayOfWeek:   dayOfWeek,
				ServiceName: serviceName,
				Location:    &location,
				Time:        timeStr,
				Occasion:    occasion,
				Notes:       nil,
				Language:    &lang,
			})
		})
	})

	return services, nil
}

func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}
