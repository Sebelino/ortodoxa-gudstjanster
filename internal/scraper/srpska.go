package scraper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/srpska"
	"ortodoxa-gudstjanster/internal/store"
	"ortodoxa-gudstjanster/internal/vision"
)

const (
	srpskaSourceName = "Sankt Sava"
	srpskaLocation   = "Sankt Sava, Bägerstavägen 68, 120 47 Enskede Gård"
	srpskaLanguage   = "Kyrkoslaviska"
	srpskaWeeks      = 26
)

// SrpskaScraper scrapes the Serbian Orthodox Church schedule.
type SrpskaScraper struct {
	visionClient *vision.Client
	store        store.Store
}

// NewSrpskaScraper creates a new scraper for the Serbian Orthodox Church.
func NewSrpskaScraper(visionClient *vision.Client, gcsStore store.Store) *SrpskaScraper {
	return &SrpskaScraper{visionClient: visionClient, store: gcsStore}
}

func (s *SrpskaScraper) Name() string {
	return srpskaSourceName
}

func (s *SrpskaScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	// Part 1: Fetch page content (table text + full body text)
	page, err := srpska.FetchPageContent(ctx)
	if err != nil {
		return nil, err
	}

	// Part 2: Parse recurring schedule from table text
	schedule, err := srpska.ParseScheduleTable(page.TableText)
	if err != nil {
		return nil, err
	}

	// Part 3: Interpret notice text for schedule exceptions
	var exceptions []srpska.ScheduleException
	if s.visionClient != nil && page.BodyText != "" {
		exceptions, err = s.interpretNotice(ctx, page.BodyText, schedule)
		if err != nil {
			log.Printf("WARNING: Failed to interpret schedule notice (proceeding without exceptions): %v", err)
			exceptions = nil
		} else if len(exceptions) > 0 {
			log.Printf("Sankt Sava: %d schedule exceptions from notice", len(exceptions))
			for _, exc := range exceptions {
				log.Printf("  %s: %d services", exc.Date, len(exc.Services))
			}
		}
	}

	// Part 4: Generate calendar events with exceptions applied
	events := srpska.GenerateEvents(schedule, srpskaWeeks, exceptions)

	// Convert to ChurchService model
	return s.toChurchServices(events), nil
}

// extractNoticeSection returns just the announcement section of the page body,
// starting from the Serbian word for "Announcement" (Обавештење). The full body
// text includes navigation and footer which may contain dynamic content (dates,
// counters) that would change the hash between runs even when the notice is the
// same. Falling back to full body text when the marker is absent.
func extractNoticeSection(bodyText string) string {
	for _, marker := range []string{"Обавештење", "ОБАВЕШТЕЊЕ", "Obaveštenje", "OBAVEŠTENJE"} {
		if idx := strings.Index(bodyText, marker); idx != -1 {
			return bodyText[idx:]
		}
	}
	return bodyText
}

// interpretNotice sends the notice section and recurring schedule to the AI
// to identify dates where the notice overrides the recurring schedule.
// Results are cached by the SHA256 of the notice section text so the same notice
// always produces the same exceptions without re-querying the AI.
func (s *SrpskaScraper) interpretNotice(ctx context.Context, bodyText string, schedule *srpska.RecurringSchedule) ([]srpska.ScheduleException, error) {
	// Build a human-readable description of the recurring schedule
	recurringDesc := ""
	for _, svc := range schedule.Services {
		recurringDesc += fmt.Sprintf("- %s on %v at %s\n", svc.Name, svc.Days, svc.Time)
	}

	// Extract only the notice section — stable across runs even if nav/footer changes.
	noticeText := extractNoticeSection(bodyText)

	// Check cache keyed by notice section checksum
	sum := sha256.Sum256([]byte(noticeText))
	cacheKey := "srpska-notice/v2/" + hex.EncodeToString(sum[:])
	if s.store != nil {
		var cached []srpska.ScheduleException
		if s.store.GetJSON(cacheKey, &cached) {
			log.Printf("Sankt Sava: notice cache hit (%s)", hex.EncodeToString(sum[:8]))
			return cached, nil
		}
		log.Printf("Sankt Sava: notice cache miss (%s), calling AI", hex.EncodeToString(sum[:8]))
	}

	// Call AI to interpret the notice
	visionExceptions, err := s.visionClient.InterpretScheduleNotice(ctx, noticeText, recurringDesc)
	if err != nil {
		return nil, err
	}

	// Convert vision.ScheduleException → srpska.ScheduleException
	var exceptions []srpska.ScheduleException
	for _, ve := range visionExceptions {
		exc := srpska.ScheduleException{
			Date: ve.Date,
		}
		for _, vs := range ve.Services {
			exc.Services = append(exc.Services, srpska.ExceptionService{
				Name: vs.Name,
				Time: vs.Time,
			})
		}
		exceptions = append(exceptions, exc)
	}

	// Store result so future runs with the same notice skip the AI call
	if s.store != nil {
		if err := s.store.SetJSON(cacheKey, exceptions); err != nil {
			log.Printf("WARNING: Failed to cache notice result: %v", err)
		}
	}

	return exceptions, nil
}

func (s *SrpskaScraper) toChurchServices(events []srpska.CalendarEvent) []model.ChurchService {
	services := make([]model.ChurchService, len(events))
	location := srpskaLocation
	lang := srpskaLanguage

	for i, event := range events {
		timeStr := event.Time
		services[i] = model.ChurchService{
			Parish:      srpskaSourceName,
			Source:      srpskaSourceName,
			SourceURL:   srpska.CalendarURL,
			Date:        event.Date,
			DayOfWeek:   event.DayOfWeek,
			ServiceName: event.ServiceName,
			Location:    &location,
			Time:        &timeStr,
			Occasion:    nil,
			Notes:       nil,
			Language:    &lang,
		}
	}

	return services
}
