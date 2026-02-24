package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const gomosScheduleURL = "https://gomos.se/en/category/schedule/"
const gomosLocation = "Stockholm, St. Georgios Cathedral, Birger Jarlsgatan 92"

func FetchGomosCalendar() ([]ChurchService, error) {
	// Find the latest schedule post
	postURL, err := findLatestSchedulePost()
	if err != nil {
		return nil, fmt.Errorf("finding latest post: %w", err)
	}

	// Get image URLs from the post
	imageURLs, err := extractImageURLs(postURL)
	if err != nil {
		return nil, fmt.Errorf("extracting images: %w", err)
	}

	var allServices []ChurchService
	for _, imgURL := range imageURLs {
		// Download and OCR the image
		text, err := downloadAndOCR(imgURL)
		if err != nil {
			continue // Skip failed images
		}

		// Parse the OCR text into services
		services := parseGomosSchedule(text)
		allServices = append(allServices, services...)
	}

	return allServices, nil
}

func findLatestSchedulePost() (string, error) {
	resp, err := http.Get(gomosScheduleURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", err
	}

	// Find the first article link (latest post)
	var postURL string
	doc.Find("article a, .entry-title a, h2 a").EachWithBreak(func(i int, s *goquery.Selection) bool {
		href, exists := s.Attr("href")
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

func extractImageURLs(postURL string) ([]string, error) {
	resp, err := http.Get(postURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	var urls []string
	doc.Find("article img, .entry-content img, .wp-block-image img").Each(func(i int, s *goquery.Selection) {
		src, exists := s.Attr("src")
		if exists && (strings.Contains(src, ".jpg") || strings.Contains(src, ".png") || strings.Contains(src, ".jpeg")) {
			urls = append(urls, src)
		}
	})

	return urls, nil
}

func downloadAndOCR(imageURL string) (string, error) {
	// Download image
	resp, err := http.Get(imageURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Save to temp file
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

	// Run tesseract
	cmd := exec.Command("tesseract", tmpFile.Name(), "stdout", "-l", "swe+eng")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tesseract failed: %w", err)
	}

	return string(output), nil
}

func parseGomosSchedule(text string) []ChurchService {
	var services []ChurchService

	lines := strings.Split(text, "\n")

	// Pattern for dates like "Söndag 1:a februari" or "Måndag 2 februari"
	datePattern := regexp.MustCompile(`(?i)(måndag|tisdag|onsdag|torsdag|fredag|lördag|söndag)\s+(\d{1,2})(?::?\w*)?\s+(januari|februari|mars|april|maj|juni|juli|augusti|september|oktober|november|december|january|february|march|april|may|june|july|august|september|october|november|december)`)
	// Time pattern like "815", "930", "1000", "1800" or "8:15", "9:30"
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

		// Check if line contains a date
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
			// Extract occasion (text after the dash)
			if dashIdx := strings.Index(line, "-"); dashIdx != -1 {
				currentOccasion = strings.TrimSpace(line[dashIdx+1:])
			} else {
				currentOccasion = ""
			}
			continue
		}

		// Look for service times and names
		if timeMatch := timePattern.FindStringSubmatch(line); timeMatch != nil && currentDate != "" {
			hour := timeMatch[1]
			minute := timeMatch[2]
			timeStr := fmt.Sprintf("%s:%s", hour, minute)

			// Extract service name (text before time)
			idx := strings.Index(line, timeMatch[0])
			serviceName := strings.TrimSpace(line[:idx])
			if serviceName == "" {
				// Try text after time
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

			services = append(services, ChurchService{
				Source:      "St. Georgios Cathedral",
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
