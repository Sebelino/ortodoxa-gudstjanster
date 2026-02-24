package main

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type ChurchService struct {
	Date        string  `json:"date"`
	DayOfWeek   string  `json:"day_of_week"`
	ServiceName string  `json:"service_name"`
	Location    *string `json:"location"`
	Time        *string `json:"time"`
	Occasion    *string `json:"occasion"`
	Notes       *string `json:"notes"`
}

func FetchCalendar(url string) ([]ChurchService, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	var services []ChurchService
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

		services = append(services, ChurchService{
			Date:        date,
			DayOfWeek:   dayOfWeek,
			ServiceName: serviceName,
			Location:    location,
			Time:        time,
			Occasion:    occasion,
			Notes:       notesPtr,
		})
	})

	return services, nil
}
