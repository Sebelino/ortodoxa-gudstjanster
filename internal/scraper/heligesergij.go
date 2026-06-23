package scraper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/store"
	"ortodoxa-gudstjanster/internal/vision"
)

const (
	heligeSergijSourceName      = "Helige Sergij rysk-ortodoxa församling"
	heligeSergijParishSlug      = "helige-sergij"
	heligeSergijURL             = "https://t.me/s/helige_sergij"
	heligeSergijDefaultLocation = "Helige Sergij Ryska Ortodoxa Församling, Solkraftsvägen 16A, 135 70 Stockholm"
	heligeSergijTextCacheKey    = "helige-sergij/latest-text"
)

// HeligeSergijScraper fetches the schedule for Helige Sergij from their Telegram channel.
type HeligeSergijScraper struct {
	store  store.Store
	vision *vision.Client
}

// NewHeligeSergijScraper creates a new scraper for Helige Sergij Ryska Ortodoxa Församling.
func NewHeligeSergijScraper(s store.Store, v *vision.Client) *HeligeSergijScraper {
	return &HeligeSergijScraper{store: s, vision: v}
}

func (s *HeligeSergijScraper) Name() string {
	return heligeSergijSourceName
}

func (s *HeligeSergijScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	var text string
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		text, err = fetchTelegramScheduleText(ctx)
		if err != nil {
			return nil, err
		}
		if text != "" {
			break
		}
		if attempt < 3 {
			log.Printf("Helige Sergij: no schedule posts found on attempt %d/3, retrying in 10s", attempt)
			time.Sleep(10 * time.Second)
		}
	}
	if text == "" {
		if cached, ok := s.store.Get(heligeSergijTextCacheKey); ok && len(cached) > 0 {
			log.Printf("Helige Sergij: Telegram unavailable, using cached schedule text")
			text = string(cached)
		} else {
			return nil, fmt.Errorf("no schedule posts found on Telegram channel")
		}
	} else {
		if err := s.store.Set(heligeSergijTextCacheKey, []byte(text)); err != nil {
			log.Printf("Helige Sergij: failed to cache schedule text: %v", err)
		}
	}

	hash := sha256.Sum256([]byte(text))
	checksum := hex.EncodeToString(hash[:])
	cacheKey := "helige-sergij/v5/" + checksum

	var entries []vision.ScheduleEntry
	if s.store.GetJSON(cacheKey, &entries) {
		return s.entriesToServices(entries), nil
	}

	entries, err = s.vision.ExtractScheduleFromRussianText(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("extracting schedule: %w", err)
	}

	if err := s.store.SetJSON(cacheKey, entries); err != nil {
		log.Printf("warning: failed to cache helige-sergij schedule: %v", err)
	}

	return s.entriesToServices(entries), nil
}

func (s *HeligeSergijScraper) entriesToServices(entries []vision.ScheduleEntry) []model.ChurchService {
	var services []model.ChurchService
	for _, e := range entries {
		location := heligeSergijDefaultLocation
		if e.Location != "" {
			location = e.Location
		}
		var timePtr *string
		if e.Time != "" {
			timePtr = &e.Time
		}
		var occasionPtr *string
		if e.Occasion != "" {
			occasionPtr = &e.Occasion
		}
		services = append(services, model.ChurchService{
			Parish:      "",
			ParishSlug:  heligeSergijParishSlug,
			Source:      heligeSergijSourceName,
			SourceURL:   heligeSergijURL,
			Date:        e.Date,
			DayOfWeek:   e.DayOfWeek,
			ServiceName: e.ServiceName,
			Location:    &location,
			Time:        timePtr,
			Occasion:    occasionPtr,
		})
	}
	return services
}

// fetchTelegramScheduleText fetches the Telegram public channel page and returns
// the combined text of posts that look like service schedule announcements.
func fetchTelegramScheduleText(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", heligeSergijURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; OrtodoxaGudstjanster/1.0)")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching Telegram page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d for %s", resp.StatusCode, heligeSergijURL)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", fmt.Errorf("parsing HTML: %w", err)
	}

	timePattern := regexp.MustCompile(`\d{1,2}[.:]\d{2}`)
	monthPattern := regexp.MustCompile(`(?i)(январ|феврал|март|апрел|ма[йя]|июн|июл|август|сентябр|октябр|ноябр|декабр)`)
	// Pin notifications start with the channel name followed by "pinned" in Russian or English
	pinnedPattern := regexp.MustCompile(`(?i) (pinned|закрепил|закрепила) «`)

	var schedulePosts []string
	doc.Find(".tgme_widget_message_text").Each(func(_ int, sel *goquery.Selection) {
		text := extractHTMLText(sel)
		if text == "" {
			return
		}
		if pinnedPattern.MatchString(text) {
			return
		}
		if timePattern.MatchString(text) && monthPattern.MatchString(text) {
			schedulePosts = append(schedulePosts, text)
		}
	})

	// Use only the most recent 2 schedule posts to avoid sending stale data to OpenAI.
	// Posts on the Telegram page are in chronological order so the last items are newest.
	if len(schedulePosts) > 2 {
		schedulePosts = schedulePosts[len(schedulePosts)-2:]
	}

	return strings.Join(schedulePosts, "\n\n---\n\n"), nil
}

// extractHTMLText converts an HTML element's content to plain text,
// treating <br> tags as newlines.
func extractHTMLText(sel *goquery.Selection) string {
	h, _ := sel.Html()
	h = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(h, "\n")
	h = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(h, "")
	return strings.TrimSpace(html.UnescapeString(h))
}
