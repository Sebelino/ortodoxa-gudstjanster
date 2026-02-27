package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

const (
	srpskaCalendarURL = "https://www.crkvastokholm.se/calendar"
)

// RecurringSchedule represents the structured schedule output
type RecurringSchedule struct {
	Services []RecurringService `json:"services"`
}

// RecurringService represents a single recurring service
type RecurringService struct {
	Name string   `json:"name"`
	Days []string `json:"days"`
	Time string   `json:"time"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	schedule, err := extractSchedule(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(schedule); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		os.Exit(1)
	}
}

func extractSchedule(ctx context.Context) (*RecurringSchedule, error) {
	// Create headless Chrome context
	opts := chromedp.DefaultExecAllocatorOptions[:]
	if chromePath := os.Getenv("CHROME_PATH"); chromePath != "" {
		opts = append(opts, chromedp.ExecPath(chromePath))
	}
	opts = append(opts,
		chromedp.Headless,
		chromedp.DisableGPU,
		chromedp.NoSandbox,
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, opts...)
	defer allocCancel()

	chromeCtx, chromeCancel := chromedp.NewContext(allocCtx)
	defer chromeCancel()

	var tableText string

	// Navigate to the calendar page and extract the schedule table
	err := chromedp.Run(chromeCtx,
		chromedp.Navigate(srpskaCalendarURL),
		// Wait for the schedule table to be rendered
		chromedp.WaitVisible(`table`, chromedp.ByQuery),
		// Give React a moment to fully render
		chromedp.Sleep(1*time.Second),
		// Extract the table text content
		chromedp.Text(`table`, &tableText, chromedp.ByQuery),
	)
	if err != nil {
		return nil, fmt.Errorf("extracting schedule table: %w", err)
	}

	// Parse the extracted text
	return parseScheduleTable(tableText)
}

func parseScheduleTable(text string) (*RecurringSchedule, error) {
	schedule := &RecurringSchedule{
		Services: []RecurringService{},
	}

	// Split into lines and process
	lines := strings.Split(text, "\n")

	// Pattern to match service entries like "Јутрење - недеља:	8:00"
	// Format: "ServiceName - days:	HH:MM" (tab-separated)
	servicePattern := regexp.MustCompile(`^(.+?)\s*[-–]\s*(.+?):\s*(\d{1,2}):(\d{2})`)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Handle tab-separated format: join with space
		line = strings.ReplaceAll(line, "\t", " ")

		matches := servicePattern.FindStringSubmatch(line)
		if len(matches) >= 5 {
			name := strings.TrimSpace(matches[1])
			daysStr := strings.TrimSpace(matches[2])
			hour := matches[3]
			minute := matches[4]

			if len(hour) == 1 {
				hour = "0" + hour
			}
			timeStr := hour + ":" + minute

			// Translate Serbian service name to Swedish
			swedishName := translateServiceName(name)
			days := parseDays(daysStr)

			if swedishName != "" && len(days) > 0 {
				schedule.Services = append(schedule.Services, RecurringService{
					Name: swedishName,
					Days: days,
					Time: timeStr,
				})
			}
		}
	}

	if len(schedule.Services) == 0 {
		return nil, fmt.Errorf("could not parse any services from table text: %q", text)
	}

	return schedule, nil
}

func translateServiceName(name string) string {
	// Serbian (Cyrillic) to Swedish translations
	translations := map[string]string{
		"Јутрење":   "Morgongudstjänst",
		"Литургија": "Helig Liturgi",
		"Вечерње":   "Aftongudstjänst",
		// Latin variants
		"Jutrenje":  "Morgongudstjänst",
		"Liturgija": "Helig Liturgi",
		"Večernje":  "Aftongudstjänst",
	}

	for serbian, swedish := range translations {
		if strings.Contains(name, serbian) {
			return swedish
		}
	}

	return name
}

func parseDays(s string) []string {
	var days []string

	// Check for "working days" patterns in various languages
	// Serbian Cyrillic: "радни дани", Latin: "radni dani"
	if strings.Contains(s, "радни дани") || strings.Contains(strings.ToLower(s), "radni dan") ||
		strings.Contains(strings.ToLower(s), "vardagar") || strings.Contains(strings.ToLower(s), "working day") {
		return []string{"måndag", "tisdag", "onsdag", "torsdag", "fredag"}
	}

	// Map of day names (Serbian Cyrillic, Serbian Latin, Swedish) to Swedish
	dayMappings := []struct {
		patterns []string
		swedish  string
	}{
		{[]string{"понедељак", "ponedeljak", "måndag"}, "måndag"},
		{[]string{"уторак", "utorak", "tisdag"}, "tisdag"},
		{[]string{"среда", "sreda", "onsdag"}, "onsdag"},
		{[]string{"четвртак", "četvrtak", "torsdag"}, "torsdag"},
		{[]string{"петак", "petak", "fredag"}, "fredag"},
		{[]string{"субота", "subota", "lördag"}, "lördag"},
		{[]string{"недеља", "nedelja", "söndag"}, "söndag"},
		{[]string{"празник", "praznik", "helgdag"}, "helgdag"},
	}

	lowerS := strings.ToLower(s)
	for _, mapping := range dayMappings {
		for _, pattern := range mapping.patterns {
			if strings.Contains(s, pattern) || strings.Contains(lowerS, strings.ToLower(pattern)) {
				// Avoid duplicates
				found := false
				for _, d := range days {
					if d == mapping.swedish {
						found = true
						break
					}
				}
				if !found {
					days = append(days, mapping.swedish)
				}
				break
			}
		}
	}

	return days
}
