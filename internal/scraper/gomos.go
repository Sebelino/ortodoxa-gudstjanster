package scraper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"church-services/internal/model"
	"church-services/internal/store"
	"church-services/internal/vision"
)

const (
	gomosSourceName  = "St. Georgios Cathedral"
	gomosScheduleURL = "https://gomos.se/en/category/schedule/"
	gomosLocation    = "Stockholm, St. Georgios Cathedral, Birger Jarlsgatan 92"
)

// GomosScraper scrapes the St. Georgios Cathedral schedule using OpenAI Vision API.
type GomosScraper struct {
	store  *store.Store
	vision *vision.Client
}

// NewGomosScraper creates a new scraper for St. Georgios Cathedral.
func NewGomosScraper(s *store.Store, v *vision.Client) *GomosScraper {
	return &GomosScraper{
		store:  s,
		vision: v,
	}
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
		services, err := s.processImage(ctx, imgURL)
		if err != nil {
			continue
		}
		allServices = append(allServices, services...)
	}

	return s.deduplicate(allServices), nil
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

func (s *GomosScraper) processImage(ctx context.Context, imageURL string) ([]model.ChurchService, error) {
	imageData, err := s.downloadImage(ctx, imageURL)
	if err != nil {
		return nil, fmt.Errorf("downloading image: %w", err)
	}

	checksum := s.computeChecksum(imageData)

	var entries []vision.ScheduleEntry
	if s.store.GetJSON(checksum, &entries) {
		return s.convertToServices(entries), nil
	}

	entries, err = s.vision.ExtractSchedule(ctx, imageData)
	if err != nil {
		return nil, fmt.Errorf("extracting schedule: %w", err)
	}

	if err := s.store.SetJSON(checksum, entries); err != nil {
		// Log error but don't fail - we still have the data
	}

	return s.convertToServices(entries), nil
}

func (s *GomosScraper) downloadImage(ctx context.Context, imageURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
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

func (s *GomosScraper) computeChecksum(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func (s *GomosScraper) convertToServices(entries []vision.ScheduleEntry) []model.ChurchService {
	var services []model.ChurchService

	for _, entry := range entries {
		location := gomosLocation
		time := entry.Time

		var occasion *string
		if entry.Occasion != "" {
			occasion = &entry.Occasion
		}

		services = append(services, model.ChurchService{
			Source:      gomosSourceName,
			Date:        entry.Date,
			DayOfWeek:   entry.DayOfWeek,
			ServiceName: entry.ServiceName,
			Location:    &location,
			Time:        &time,
			Occasion:    occasion,
			Notes:       nil,
		})
	}

	return services
}

// deduplicate removes duplicate services based on date, time, and similar service names.
func (s *GomosScraper) deduplicate(services []model.ChurchService) []model.ChurchService {
	if len(services) == 0 {
		return services
	}

	seen := make(map[string]bool)
	var result []model.ChurchService

	for _, svc := range services {
		timeStr := ""
		if svc.Time != nil {
			timeStr = *svc.Time
		}
		normalizedName := strings.ToLower(strings.Join(strings.Fields(svc.ServiceName), " "))
		key := fmt.Sprintf("%s|%s|%s", svc.Date, timeStr, normalizedName)

		if !seen[key] {
			seen[key] = true
			result = append(result, svc)
		}
	}

	return result
}
