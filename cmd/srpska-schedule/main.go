package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	srpskaBaseURL = "https://www.crkvastokholm.se"
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	// First, fetch the main page to find the JS bundle URL
	htmlBody, err := fetchURL(ctx, srpskaBaseURL)
	if err != nil {
		return nil, fmt.Errorf("fetching main page: %w", err)
	}

	// Extract JS bundle URL
	jsURLRegex := regexp.MustCompile(`src="(/assets/index-[^"]+\.js)"`)
	matches := jsURLRegex.FindStringSubmatch(string(htmlBody))
	if len(matches) < 2 {
		return nil, fmt.Errorf("could not find JS bundle URL")
	}
	jsURL := srpskaBaseURL + matches[1]

	// Fetch the JS bundle
	jsBody, err := fetchURL(ctx, jsURL)
	if err != nil {
		return nil, fmt.Errorf("fetching JS bundle: %w", err)
	}
	jsContent := string(jsBody)

	// Extract Swedish translations
	translations := extractSwedishTranslations(jsContent)

	schedule := &RecurringSchedule{
		Services: []RecurringService{},
	}

	// Parse footer entries which have both days and times
	// footer.svetaLiturgija1: "Söndag, lördag, helgdag: kl. 9:00" (Liturgy)
	// footer.svetaLiturgija2: "Daglig morgongudstjänst: kl. 9:00"
	// footer.svetaLiturgija3: "Daglig aftongudstjänst: kl. 17:00"

	if val, ok := translations["footer.svetaLiturgija1"]; ok {
		if svc := parseFooterEntry("Helig Liturgi", val); svc != nil {
			schedule.Services = append(schedule.Services, *svc)
		}
	}

	if val, ok := translations["footer.svetaLiturgija2"]; ok {
		if svc := parseFooterEntry("", val); svc != nil {
			schedule.Services = append(schedule.Services, *svc)
		}
	}

	if val, ok := translations["footer.svetaLiturgija3"]; ok {
		if svc := parseFooterEntry("", val); svc != nil {
			schedule.Services = append(schedule.Services, *svc)
		}
	}

	// Also check calendar.table for more detailed day information
	// These may provide more specific days than the footer
	calendarServices := extractCalendarTableServices(translations)
	if len(calendarServices) > 0 {
		// Use calendar table data if it provides more detail
		schedule.Services = mergeSchedules(schedule.Services, calendarServices)
	}

	if len(schedule.Services) == 0 {
		return nil, fmt.Errorf("could not extract any schedule information")
	}

	return schedule, nil
}

func extractSwedishTranslations(jsContent string) map[string]string {
	translations := make(map[string]string)

	// Match all string key-value pairs
	keyPattern := regexp.MustCompile(`"([a-zA-Z0-9_.]+)":"([^"]+)"`)
	matches := keyPattern.FindAllStringSubmatch(jsContent, -1)

	for _, match := range matches {
		if len(match) >= 3 {
			key := match[1]
			value := match[2]
			// Only keep Swedish translations
			if isSwedish(value) {
				translations[key] = value
			}
		}
	}

	return translations
}

func isSwedish(s string) bool {
	swedishIndicators := []string{
		"lördag", "söndag", "måndag", "tisdag", "onsdag", "torsdag", "fredag",
		"helgdag", "Morgon", "Afton", "kl.", "Helig", "Daglig",
		"ö", "ä", "å", // Swedish letters
	}
	lowerS := strings.ToLower(s)
	for _, indicator := range swedishIndicators {
		if strings.Contains(lowerS, strings.ToLower(indicator)) {
			return true
		}
	}
	return false
}

func parseFooterEntry(defaultName, value string) *RecurringService {
	svc := &RecurringService{}

	// Extract time
	timeRegex := regexp.MustCompile(`kl\.?\s*(\d{1,2}):(\d{2})`)
	timeMatch := timeRegex.FindStringSubmatch(value)
	if len(timeMatch) >= 3 {
		hour := timeMatch[1]
		if len(hour) == 1 {
			hour = "0" + hour
		}
		svc.Time = hour + ":" + timeMatch[2]
	}

	// Extract name from the value if not provided
	if defaultName != "" {
		svc.Name = defaultName
	} else {
		// Try to extract name from value (text before ":")
		if colonIdx := strings.Index(value, ":"); colonIdx != -1 {
			namePart := strings.TrimSpace(value[:colonIdx])
			// Check if it's actually a name (not days)
			if !containsDay(namePart) {
				svc.Name = namePart
			}
		}
	}

	// Extract days
	svc.Days = extractDays(value)

	// Handle "Daglig" (daily)
	if strings.Contains(strings.ToLower(value), "daglig") {
		svc.Days = []string{"måndag", "tisdag", "onsdag", "torsdag", "fredag", "lördag", "söndag"}
	}

	if svc.Time != "" && (len(svc.Days) > 0 || svc.Name != "") {
		return svc
	}

	return nil
}

func containsDay(s string) bool {
	days := []string{"måndag", "tisdag", "onsdag", "torsdag", "fredag", "lördag", "söndag", "helgdag"}
	lowerS := strings.ToLower(s)
	for _, day := range days {
		if strings.Contains(lowerS, day) {
			return true
		}
	}
	return false
}

func extractCalendarTableServices(translations map[string]string) []RecurringService {
	var services []RecurringService

	// calendar.table.time1: "Morgongudstjänst - lördag, söndag och helgdag:"
	// calendar.table.time2: "Hellig liturgi - lördag, söndag och helgdag:"
	// calendar.table.time3: "Morgongudstjänst - måndag och fredag:"
	// calendar.table.time4: "Aftongudstjänst - måndag och fredag:"

	for i := 1; i <= 4; i++ {
		key := fmt.Sprintf("calendar.table.time%d", i)
		if val, ok := translations[key]; ok {
			svc := parseCalendarTableEntry(val)
			if svc != nil {
				services = append(services, *svc)
			}
		}
	}

	return services
}

func parseCalendarTableEntry(value string) *RecurringService {
	svc := &RecurringService{}

	// Extract name (text before " - ")
	if dashIdx := strings.Index(value, " - "); dashIdx != -1 {
		svc.Name = strings.TrimSpace(value[:dashIdx])
	}

	// Extract days
	svc.Days = extractDays(value)

	if svc.Name != "" && len(svc.Days) > 0 {
		return svc
	}

	return nil
}

func extractDays(s string) []string {
	days := []string{}
	lowerS := strings.ToLower(s)

	dayList := []string{"måndag", "tisdag", "onsdag", "torsdag", "fredag", "lördag", "söndag", "helgdag"}

	for _, day := range dayList {
		if strings.Contains(lowerS, day) {
			days = append(days, day)
		}
	}

	return days
}

func mergeSchedules(footerServices, calendarServices []RecurringService) []RecurringService {
	// Create a map of calendar services by normalized name
	calendarByName := make(map[string]*RecurringService)
	for i := range calendarServices {
		name := strings.ToLower(calendarServices[i].Name)
		calendarByName[name] = &calendarServices[i]
	}

	// Merge times from footer into calendar services
	result := []RecurringService{}

	for _, footer := range footerServices {
		footerNameLower := strings.ToLower(footer.Name)

		// Try to find matching calendar entry
		var matched *RecurringService
		for name, cal := range calendarByName {
			if strings.Contains(footerNameLower, name) || strings.Contains(name, footerNameLower) {
				matched = cal
				break
			}
			// Check for partial matches
			if strings.Contains(footerNameLower, "liturgi") && strings.Contains(name, "liturgi") {
				matched = cal
				break
			}
			if strings.Contains(footerNameLower, "morgon") && strings.Contains(name, "morgon") {
				matched = cal
				break
			}
			if strings.Contains(footerNameLower, "afton") && strings.Contains(name, "afton") {
				matched = cal
				break
			}
		}

		if matched != nil && matched.Time == "" {
			// Use calendar days with footer time
			merged := RecurringService{
				Name: matched.Name,
				Days: matched.Days,
				Time: footer.Time,
			}
			result = append(result, merged)
			delete(calendarByName, strings.ToLower(matched.Name))
		} else {
			// Use footer entry as-is
			result = append(result, footer)
		}
	}

	// Add any remaining calendar services without times
	for _, cal := range calendarByName {
		if cal.Time != "" || len(cal.Days) > 0 {
			result = append(result, *cal)
		}
	}

	return result
}

func fetchURL(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}
