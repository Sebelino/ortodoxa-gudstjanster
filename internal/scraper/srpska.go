package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/smtp"
	"os"
	"regexp"
	"strings"
	"time"

	"ortodoxa-gudstjanster/internal/model"
)

const (
	srpskaSourceName = "Srpska Pravoslavna Crkva Sveti Sava"
	srpskaURL        = "https://www.crkvastokholm.se/calendar"
	srpskaLocation   = "Stockholm, Bägerstavägen 68"
	srpskaLanguage   = "Serbiska, svenska"
)

// Expected schedule - if this changes on the website, send notification
var expectedSrpskaSchedule = []srpskaService{
	{DayOfWeek: "Sunday", Opens: "10:00", Closes: "12:00", ServiceName: "Helig Liturgi"},
}

type srpskaService struct {
	DayOfWeek   string
	Opens       string
	Closes      string
	ServiceName string
}

// SrpskaScraper scrapes the Serbian Orthodox Church schedule.
type SrpskaScraper struct {
	notifyEmail string
	smtpHost    string
	smtpPort    string
	smtpUser    string
	smtpPass    string
}

// NewSrpskaScraper creates a new scraper for the Serbian Orthodox Church.
func NewSrpskaScraper() *SrpskaScraper {
	return &SrpskaScraper{
		notifyEmail: "sebelino7+ortodoxa-gudstjanster@gmail.com",
		smtpHost:    os.Getenv("SMTP_HOST"),
		smtpPort:    os.Getenv("SMTP_PORT"),
		smtpUser:    os.Getenv("SMTP_USER"),
		smtpPass:    os.Getenv("SMTP_PASS"),
	}
}

func (s *SrpskaScraper) Name() string {
	return srpskaSourceName
}

func (s *SrpskaScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	// Fetch the page and check for schedule changes
	bodyBytes, err := fetchURL(ctx, srpskaURL)
	if err != nil {
		return nil, fmt.Errorf("fetching page: %w", err)
	}

	// Extract and verify schedule from JSON-LD
	currentSchedule, err := s.extractScheduleFromPage(string(bodyBytes))
	if err != nil {
		// If we can't parse, log but continue with expected schedule
		fmt.Printf("warning: could not parse srpska schedule: %v\n", err)
	} else {
		// Check if schedule has changed
		if !s.schedulesMatch(currentSchedule, expectedSrpskaSchedule) {
			s.sendScheduleChangeNotification(currentSchedule)
		}
	}

	// Generate recurring events for the next 8 weeks
	return s.generateRecurringEvents(), nil
}

func (s *SrpskaScraper) extractScheduleFromPage(html string) ([]srpskaService, error) {
	// Find JSON-LD script
	jsonLDRegex := regexp.MustCompile(`<script type="application/ld\+json">\s*(\{[\s\S]*?\})\s*</script>`)
	matches := jsonLDRegex.FindStringSubmatch(html)
	if len(matches) < 2 {
		return nil, fmt.Errorf("no JSON-LD found")
	}

	var data struct {
		OpeningHoursSpecification []struct {
			DayOfWeek string `json:"dayOfWeek"`
			Opens     string `json:"opens"`
			Closes    string `json:"closes"`
		} `json:"openingHoursSpecification"`
	}

	if err := json.Unmarshal([]byte(matches[1]), &data); err != nil {
		return nil, fmt.Errorf("parsing JSON-LD: %w", err)
	}

	var schedule []srpskaService
	for _, spec := range data.OpeningHoursSpecification {
		schedule = append(schedule, srpskaService{
			DayOfWeek:   spec.DayOfWeek,
			Opens:       spec.Opens,
			Closes:      spec.Closes,
			ServiceName: s.inferServiceName(spec.DayOfWeek, spec.Opens),
		})
	}

	return schedule, nil
}

func (s *SrpskaScraper) inferServiceName(dayOfWeek, opens string) string {
	if dayOfWeek == "Sunday" && opens == "10:00" {
		return "Helig Liturgi"
	}
	if dayOfWeek == "Saturday" {
		return "Kvällsgudstjänst"
	}
	return "Gudstjänst"
}

func (s *SrpskaScraper) schedulesMatch(current, expected []srpskaService) bool {
	if len(current) != len(expected) {
		return false
	}

	for i := range current {
		if current[i].DayOfWeek != expected[i].DayOfWeek ||
			current[i].Opens != expected[i].Opens ||
			current[i].Closes != expected[i].Closes {
			return false
		}
	}

	return true
}

func (s *SrpskaScraper) sendScheduleChangeNotification(newSchedule []srpskaService) {
	if s.smtpHost == "" || s.smtpUser == "" || s.smtpPass == "" {
		fmt.Printf("WARNING: Srpska church schedule has changed but SMTP not configured!\n")
		fmt.Printf("New schedule: %+v\n", newSchedule)
		return
	}

	subject := "Srpska Pravoslavna Crkva - Schema ändrat!"
	body := fmt.Sprintf(`Schemat för Srpska Pravoslavna Crkva Sveti Sava har ändrats på hemsidan.

Nytt schema från hemsidan:
%s

Förväntat schema:
%s

Vänligen uppdatera expectedSrpskaSchedule i srpska.go om det nya schemat är korrekt.

Källa: %s
`, s.formatSchedule(newSchedule), s.formatSchedule(expectedSrpskaSchedule), srpskaURL)

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		s.smtpUser, s.notifyEmail, subject, body)

	auth := smtp.PlainAuth("", s.smtpUser, s.smtpPass, s.smtpHost)
	addr := s.smtpHost + ":" + s.smtpPort

	if err := smtp.SendMail(addr, auth, s.smtpUser, []string{s.notifyEmail}, []byte(msg)); err != nil {
		fmt.Printf("ERROR: Failed to send schedule change notification: %v\n", err)
	} else {
		fmt.Printf("Sent schedule change notification to %s\n", s.notifyEmail)
	}
}

func (s *SrpskaScraper) formatSchedule(schedule []srpskaService) string {
	var lines []string
	for _, svc := range schedule {
		lines = append(lines, fmt.Sprintf("  - %s: %s-%s (%s)", svc.DayOfWeek, svc.Opens, svc.Closes, svc.ServiceName))
	}
	return strings.Join(lines, "\n")
}

func (s *SrpskaScraper) generateRecurringEvents() []model.ChurchService {
	var services []model.ChurchService

	now := time.Now()
	// Start from today
	current := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	// Generate for 8 weeks
	end := current.AddDate(0, 0, 8*7)

	location := srpskaLocation
	lang := srpskaLanguage

	for current.Before(end) {
		// Sunday Liturgy at 10:00
		if current.Weekday() == time.Sunday {
			timeStr := "10:00"
			services = append(services, model.ChurchService{
				Source:      srpskaSourceName,
				SourceURL:   srpskaURL,
				Date:        current.Format("2006-01-02"),
				DayOfWeek:   s.weekdayToSwedish(current.Weekday()),
				ServiceName: "Helig Liturgi",
				Location:    &location,
				Time:        &timeStr,
				Occasion:    nil,
				Notes:       nil,
				Language:    &lang,
			})
		}

		current = current.AddDate(0, 0, 1)
	}

	return services
}

func (s *SrpskaScraper) weekdayToSwedish(day time.Weekday) string {
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
