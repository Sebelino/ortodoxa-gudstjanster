package scraper

import (
	"context"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"church-services/internal/model"
)

const (
	finskaSourceName = "Finska Ortodoxa Församlingen"
	finskaDefaultURL = "https://www.ortodox-finsk.se/kalender/"
	finskaLanguage   = "Svenska, finska"
)

// FisnkaScraper scrapes the Finnish Orthodox Congregation calendar.
type FisnkaScraper struct {
	url string
}

// NewFinskaScraper creates a new scraper for the Finnish Orthodox Congregation.
func NewFinskaScraper(url string) *FisnkaScraper {
	if url == "" {
		url = finskaDefaultURL
	}
	return &FisnkaScraper{url: url}
}

func (s *FisnkaScraper) Name() string {
	return finskaSourceName
}

func (s *FisnkaScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	doc, err := fetchDocument(ctx, s.url)
	if err != nil {
		return nil, err
	}

	var services []model.ChurchService
	dateRegex := regexp.MustCompile(`(\d{4}-\d{2}-\d{2})\s*\|\s*(\S+)`)

	doc.Find("section.calendar div.calendar-item").Each(func(i int, item *goquery.Selection) {
		meta := item.Find("div.meta").Text()
		matches := dateRegex.FindStringSubmatch(meta)
		if len(matches) < 3 {
			return
		}

		date := matches[1]
		dayOfWeek := matches[2]

		contentDiv := item.Find("div.calendar-item-content")
		serviceName := strings.TrimSpace(contentDiv.Find("h3").Text())
		if serviceName == "" {
			serviceName = "Unknown"
		}

		var location, time, occasion *string
		var notes []string

		detailsDiv := contentDiv.Find("div").First()
		detailsHTML, _ := detailsDiv.Html()

		// Extract location
		locRegex := regexp.MustCompile(`<strong>\s*Plats:\s*</strong>\s*([^<]+)`)
		if locMatch := locRegex.FindStringSubmatch(detailsHTML); len(locMatch) > 1 {
			loc := strings.TrimSpace(locMatch[1])
			location = &loc
		}

		// Extract time
		timeRegex := regexp.MustCompile(`<strong>\s*Tid:\s*</strong>\s*([^<]+)`)
		if timeMatch := timeRegex.FindStringSubmatch(detailsHTML); len(timeMatch) > 1 {
			t := strings.TrimSpace(timeMatch[1])
			time = &t
		}

		// Extract occasion (first strong tag that's not Plats/Tid)
		detailsDiv.Find("strong").Each(func(j int, strong *goquery.Selection) {
			if occasion != nil {
				return
			}
			text := strings.TrimSpace(strong.Text())
			if text != "" && text != "Plats:" && text != "Tid:" {
				occasion = &text
			}
		})

		// Extract notes from <p> tags
		detailsDiv.Find("p").Each(func(j int, p *goquery.Selection) {
			text := strings.TrimSpace(p.Text())
			if text != "" {
				notes = append(notes, text)
			}
		})

		var notesPtr *string
		if len(notes) > 0 {
			joined := strings.Join(notes, "\n")
			notesPtr = &joined
		}

		lang := finskaLanguage
		services = append(services, model.ChurchService{
			Source:      finskaSourceName,
			SourceURL:   s.url,
			Date:        date,
			DayOfWeek:   dayOfWeek,
			ServiceName: map[string]string{"sv": serviceName},
			Location:    location,
			Time:        time,
			Occasion:    occasion,
			Notes:       notesPtr,
			Language:    &lang,
		})
	})

	return services, nil
}
