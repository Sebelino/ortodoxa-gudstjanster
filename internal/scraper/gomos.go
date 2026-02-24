package scraper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"church-services/internal/model"
)

const (
	gomosSourceName  = "St. Georgios Cathedral"
	gomosScheduleURL = "https://gomos.se/en/category/schedule/"
	gomosLocation    = "Stockholm, St. Georgios Cathedral, Birger Jarlsgatan 92"
)

// GomosScraper scrapes the St. Georgios Cathedral schedule using OCR.
type GomosScraper struct{}

// NewGomosScraper creates a new scraper for St. Georgios Cathedral.
func NewGomosScraper() *GomosScraper {
	return &GomosScraper{}
}

func (s *GomosScraper) Name() string {
	return gomosSourceName
}

func (s *GomosScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	postURL, err := s.findLatestSchedulePost(ctx)
	if err != nil {
		return nil, fmt.Errorf("finding latest post: %w", err)
	}

	imageURLs, err := s.extractImageURLs(ctx, postURL)
	if err != nil {
		return nil, fmt.Errorf("extracting images: %w", err)
	}

	var allServices []model.ChurchService
	for _, imgURL := range imageURLs {
		text, err := s.downloadAndOCR(ctx, imgURL)
		if err != nil {
			continue
		}

		services := s.parseSchedule(text)
		allServices = append(allServices, services...)
	}

	return allServices, nil
}

func (s *GomosScraper) findLatestSchedulePost(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", gomosScheduleURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", err
	}

	var postURL string
	doc.Find("article a, .entry-title a, h2 a").EachWithBreak(func(i int, sel *goquery.Selection) bool {
		href, exists := sel.Attr("href")
		if exists && strings.Contains(href, "schedule") {
			postURL = href
			return false
		}
		return true
	})

	if postURL == "" {
		return "", fmt.Errorf("no schedule post found")
	}

	return postURL, nil
}

func (s *GomosScraper) extractImageURLs(ctx context.Context, postURL string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", postURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	var urls []string
	doc.Find("article img, .entry-content img, .wp-block-image img").Each(func(i int, sel *goquery.Selection) {
		src, exists := sel.Attr("src")
		if exists && (strings.Contains(src, ".jpg") || strings.Contains(src, ".png") || strings.Contains(src, ".jpeg")) {
			urls = append(urls, src)
		}
	})

	return urls, nil
}

func (s *GomosScraper) downloadAndOCR(ctx context.Context, imageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	tmpFile, err := os.CreateTemp("", "schedule-*.jpg")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	_, err = io.Copy(tmpFile, resp.Body)
	if err != nil {
		return "", err
	}
	tmpFile.Close()

	cmd := exec.CommandContext(ctx, "tesseract", tmpFile.Name(), "stdout", "-l", "swe+eng")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tesseract failed: %w", err)
	}

	return string(output), nil
}

func (s *GomosScraper) parseSchedule(text string) []model.ChurchService {
	var services []model.ChurchService

	lines := strings.Split(text, "\n")

	datePattern := regexp.MustCompile(`(?i)(måndag|tisdag|onsdag|torsdag|fredag|lördag|söndag)\s+(\d{1,2})(?::?\w*)?\s+(januari|februari|mars|april|maj|juni|juli|augusti|september|oktober|november|december|january|february|march|april|may|june|july|august|september|october|november|december)`)
	timePattern := regexp.MustCompile(`\b(\d{1,2}):?(\d{2})\b`)

	monthMap := map[string]string{
		"januari": "01", "january": "01",
		"februari": "02", "february": "02",
		"mars": "03", "march": "03",
		"april": "04",
		"maj": "05", "may": "05",
		"juni": "06", "june": "06",
		"juli": "07", "july": "07",
		"augusti": "08", "august": "08",
		"september": "09",
		"oktober": "10", "october": "10",
		"november": "11",
		"december": "12",
	}

	var currentDate string
	var currentDayOfWeek string
	var currentOccasion string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if dateMatch := datePattern.FindStringSubmatch(line); dateMatch != nil {
			currentDayOfWeek = dateMatch[1]
			day := dateMatch[2]
			monthStr := strings.ToLower(dateMatch[3])
			if month, ok := monthMap[monthStr]; ok {
				currentDate = fmt.Sprintf("2026-%s-%02s", month, day)
				if len(day) == 1 {
					currentDate = fmt.Sprintf("2026-%s-0%s", month, day)
				}
			}
			if dashIdx := strings.Index(line, "-"); dashIdx != -1 {
				currentOccasion = strings.TrimSpace(line[dashIdx+1:])
			} else {
				currentOccasion = ""
			}
			continue
		}

		if timeMatch := timePattern.FindStringSubmatch(line); timeMatch != nil && currentDate != "" {
			hour := timeMatch[1]
			minute := timeMatch[2]
			timeStr := fmt.Sprintf("%s:%s", hour, minute)

			idx := strings.Index(line, timeMatch[0])
			serviceName := strings.TrimSpace(line[:idx])
			if serviceName == "" {
				serviceName = strings.TrimSpace(line[idx+len(timeMatch[0]):])
			}
			if serviceName == "" {
				continue
			}

			location := gomosLocation
			var occasionPtr *string
			if currentOccasion != "" {
				occasionPtr = &currentOccasion
			}

			services = append(services, model.ChurchService{
				Source:      gomosSourceName,
				Date:        currentDate,
				DayOfWeek:   currentDayOfWeek,
				ServiceName: serviceName,
				Location:    &location,
				Time:        &timeStr,
				Occasion:    occasionPtr,
				Notes:       nil,
			})
		}
	}

	return services
}
