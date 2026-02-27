package srpska

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

const (
	CalendarURL = "https://www.crkvastokholm.se/calendar"
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

// Part 1: Fetch raw table text from the website using chromedp
func FetchScheduleTable(ctx context.Context) (string, error) {
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
		chromedp.Navigate(CalendarURL),
		// Wait for the schedule table to be rendered
		chromedp.WaitVisible(`table`, chromedp.ByQuery),
		// Give React a moment to fully render
		chromedp.Sleep(1*time.Second),
		// Extract the table text content
		chromedp.Text(`table`, &tableText, chromedp.ByQuery),
	)
	if err != nil {
		return "", fmt.Errorf("extracting schedule table: %w", err)
	}

	return tableText, nil
}

// Part 2: Parse raw table text into structured schedule
func ParseScheduleTable(text string) (*RecurringSchedule, error) {
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

// CalendarEvent represents a single calendar event
type CalendarEvent struct {
	Date        string `json:"date"`
	DayOfWeek   string `json:"day_of_week"`
	ServiceName string `json:"service_name"`
	Time        string `json:"time"`
}

// Part 3: Generate calendar events from structured schedule
func GenerateEvents(schedule *RecurringSchedule, weeks int) []CalendarEvent {
	var events []CalendarEvent

	now := time.Now()
	// Start from today
	current := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	// Generate for specified weeks
	end := current.AddDate(0, 0, weeks*7)

	// Build a map of weekday name to time.Weekday
	weekdayMap := map[string]time.Weekday{
		"måndag":  time.Monday,
		"tisdag":  time.Tuesday,
		"onsdag":  time.Wednesday,
		"torsdag": time.Thursday,
		"fredag":  time.Friday,
		"lördag":  time.Saturday,
		"söndag":  time.Sunday,
	}

	for current.Before(end) {
		currentWeekday := current.Weekday()

		for _, svc := range schedule.Services {
			// Check if this service runs on the current weekday
			shouldInclude := false
			for _, day := range svc.Days {
				if day == "helgdag" {
					// Skip holidays for now - we don't have a holiday calendar
					continue
				}
				if wd, ok := weekdayMap[day]; ok && wd == currentWeekday {
					shouldInclude = true
					break
				}
			}

			if shouldInclude {
				events = append(events, CalendarEvent{
					Date:        current.Format("2006-01-02"),
					DayOfWeek:   weekdayToSwedish(currentWeekday),
					ServiceName: svc.Name,
					Time:        svc.Time,
				})
			}
		}

		current = current.AddDate(0, 0, 1)
	}

	return events
}

func weekdayToSwedish(day time.Weekday) string {
	switch day {
	case time.Monday:
		return "Måndag"
	case time.Tuesday:
		return "Tisdag"
	case time.Wednesday:
		return "Onsdag"
	case time.Thursday:
		return "Torsdag"
	case time.Friday:
		return "Fredag"
	case time.Saturday:
		return "Lördag"
	case time.Sunday:
		return "Söndag"
	default:
		return ""
	}
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
