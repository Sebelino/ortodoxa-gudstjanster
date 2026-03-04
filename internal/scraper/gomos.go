package scraper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/store"
	"ortodoxa-gudstjanster/internal/vision"
)

const (
	gomosSourceName  = "St. Georgios Cathedral"
	gomosScheduleURL = "https://gomos.se/en/category/schedule/"
	gomosLocation    = "Stockholm, St. Georgios Cathedral, Birger Jarlsgatan 92"
	gomosLanguage    = "Grekiska, svenska"
)

// GomosScraper scrapes the St. Georgios Cathedral schedule using OpenAI Vision API.
type GomosScraper struct {
	store        store.Store
	vision       *vision.Client
	uploadReader *store.BucketReader
	uploadPrefix string
}

// NewGomosScraper creates a new scraper for St. Georgios Cathedral.
func NewGomosScraper(s store.Store, v *vision.Client) *GomosScraper {
	return &GomosScraper{
		store:  s,
		vision: v,
	}
}

// SetUploadSource configures a GCS bucket as a fallback image source.
func (s *GomosScraper) SetUploadSource(reader *store.BucketReader, prefix string) {
	s.uploadReader = reader
	s.uploadPrefix = prefix
}

func (s *GomosScraper) Name() string {
	return gomosSourceName
}

func (s *GomosScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	// Collect images from all sources
	var allImages []imageWithData

	websiteImages, websiteErr := s.fetchWebsiteImages(ctx)
	if websiteErr != nil {
		log.Printf("Gomos: website failed: %v", websiteErr)
	}
	allImages = append(allImages, websiteImages...)

	if s.uploadReader != nil {
		bucketImages, bucketErr := s.fetchBucketImages(ctx)
		if bucketErr != nil {
			log.Printf("Gomos: bucket failed: %v", bucketErr)
		}
		allImages = append(allImages, bucketImages...)
	}

	if len(allImages) == 0 {
		if websiteErr != nil {
			return nil, websiteErr
		}
		return nil, fmt.Errorf("no images found")
	}

	// Process all images together: raw OCR, deduplicate by fingerprint (prefer Swedish), translate if needed
	services, err := s.processImages(ctx, allImages)
	if err != nil {
		return nil, err
	}

	return s.deduplicate(services), nil
}

// imageWithData pairs downloaded image bytes with source metadata.
type imageWithData struct {
	data      []byte
	sourceRef string // URL or bucket object name
	sourceURL string // the URL to use as source in the service
}

// fetchWebsiteImages downloads schedule images from the gomos.se website.
func (s *GomosScraper) fetchWebsiteImages(ctx context.Context) ([]imageWithData, error) {
	postURL, err := s.findLatestSchedulePost(ctx)
	if err != nil {
		return nil, fmt.Errorf("finding latest post: %w", err)
	}

	imageURLs, err := s.extractImageURLs(ctx, postURL)
	if err != nil {
		return nil, fmt.Errorf("extracting images: %w", err)
	}

	var images []imageWithData
	for _, url := range imageURLs {
		data, err := s.downloadImage(ctx, url)
		if err != nil {
			log.Printf("Gomos: failed to download %s: %v", url, err)
			continue
		}
		images = append(images, imageWithData{
			data:      data,
			sourceRef: url,
			sourceURL: gomosScheduleURL,
		})
	}

	return images, nil
}

// fetchBucketImages reads schedule images from the upload bucket.
func (s *GomosScraper) fetchBucketImages(ctx context.Context) ([]imageWithData, error) {
	names, err := s.uploadReader.ListObjects(ctx, s.uploadPrefix)
	if err != nil {
		return nil, fmt.Errorf("listing upload objects: %w", err)
	}

	var images []imageWithData
	for _, name := range names {
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".jpg") && !strings.HasSuffix(lower, ".jpeg") && !strings.HasSuffix(lower, ".png") {
			continue
		}

		imageData, err := s.uploadReader.ReadObject(ctx, name)
		if err != nil {
			log.Printf("Gomos: failed to read upload %s: %v", name, err)
			continue
		}

		images = append(images, imageWithData{
			data:      imageData,
			sourceRef: name,
			sourceURL: gomosScheduleURL,
		})
	}

	return images, nil
}

// ocrResult pairs a raw OCR result with the source URL for the image.
type ocrResult struct {
	raw       *vision.RawScheduleResult
	sourceURL string
}

// processImages is the core pipeline: raw OCR each image, deduplicate by month,
// then merge, convert, or translate surviving entries to Swedish.
func (s *GomosScraper) processImages(ctx context.Context, images []imageWithData) ([]model.ChurchService, error) {

	// Step 1: Raw OCR each image (cached)
	var results []ocrResult
	for _, img := range images {
		raw, err := s.rawOCR(ctx, img.data, img.sourceRef)
		if err != nil {
			log.Printf("Gomos: raw OCR failed for %s: %v", img.sourceRef, err)
			continue
		}
		results = append(results, ocrResult{raw: raw, sourceURL: img.sourceURL})
	}

	// Step 2: Group by month (images covering the same month are the same schedule)
	type group struct {
		items []ocrResult
	}
	groups := make(map[string]*group)
	var order []string // preserve insertion order
	for _, r := range results {
		month := scheduleMonth(r.raw.Entries)
		if _, ok := groups[month]; !ok {
			groups[month] = &group{}
			order = append(order, month)
		}
		groups[month].items = append(groups[month].items, r)
	}

	// Step 3+4: Per group, merge or convert entries
	var allServices []model.ChurchService
	for _, month := range order {
		g := groups[month]

		var entries []vision.ScheduleEntry
		sourceURL := g.items[0].sourceURL

		if len(g.items) > 1 {
			// Multiple images for the same month: merge via AI
			var err error
			entries, err = s.mergeEntries(ctx, g.items)
			if err != nil {
				log.Printf("Gomos: merge failed for %s: %v", month, err)
				continue
			}
		} else {
			// Single image: convert directly or translate
			chosen := g.items[0]
			sourceURL = chosen.sourceURL
			lang := strings.ToLower(chosen.raw.Language)
			if lang == "swedish" || lang == "svenska" {
				entries = rawEntriesToSwedish(chosen.raw.Entries)
			} else {
				var err error
				entries, err = s.translateEntries(ctx, chosen.raw.Entries)
				if err != nil {
					log.Printf("Gomos: translation failed: %v", err)
					continue
				}
			}
		}

		allServices = append(allServices, s.convertToServices(entries, sourceURL)...)
	}

	return allServices, nil
}

// rawOCR extracts schedule entries from an image in the original language, with caching.
func (s *GomosScraper) rawOCR(ctx context.Context, imageData []byte, sourceRef string) (*vision.RawScheduleResult, error) {
	checksum := s.computeChecksum(imageData)
	cacheKey := "raw-ocr/" + checksum

	var cached vision.RawScheduleResult
	if s.store.GetJSON(cacheKey, &cached) {
		log.Printf("Gomos: raw OCR cache hit for %s", sourceRef)
		return &cached, nil
	}

	result, rawResponse, err := s.vision.ExtractScheduleRaw(ctx, imageData)
	if err != nil {
		return nil, fmt.Errorf("raw OCR for %s: %w", sourceRef, err)
	}

	// Persist structured result
	if data, merr := json.Marshal(result); merr == nil {
		if werr := s.store.SetRaw(cacheKey+".json", data); werr != nil {
			log.Printf("Gomos: failed to cache raw OCR result: %v", werr)
		}
	}

	// Persist source image
	imageExt := s.imageExtension(sourceRef)
	if werr := s.store.SetRaw(cacheKey+imageExt, imageData); werr != nil {
		log.Printf("Gomos: failed to persist source image: %v", werr)
	}

	// Persist raw API response
	if werr := s.store.SetRaw(cacheKey+".response.txt", []byte(rawResponse)); werr != nil {
		log.Printf("Gomos: failed to persist raw response: %v", werr)
	}

	return result, nil
}

// scheduleMonth returns the most common year-month (YYYY-MM) among entries,
// used to group images covering the same monthly schedule.
func scheduleMonth(entries []vision.RawScheduleEntry) string {
	counts := make(map[string]int)
	for _, e := range entries {
		if len(e.Date) >= 7 {
			counts[e.Date[:7]]++
		}
	}
	best := ""
	bestN := 0
	for m, n := range counts {
		if n > bestN {
			best = m
			bestN = n
		}
	}
	return best
}

// mergeEntries merges multiple OCR results (same schedule, different languages) via the OpenAI API, with caching.
func (s *GomosScraper) mergeEntries(ctx context.Context, items []ocrResult) ([]vision.ScheduleEntry, error) {
	// Collect all raw schedule results
	schedules := make([]vision.RawScheduleResult, len(items))
	for i, item := range items {
		schedules[i] = *item.raw
	}

	// Compute cache key from combined inputs
	inputJSON, err := json.Marshal(schedules)
	if err != nil {
		return nil, fmt.Errorf("marshaling schedules: %w", err)
	}
	hash := sha256.Sum256(inputJSON)
	hashStr := hex.EncodeToString(hash[:])
	cacheKey := "merge/" + hashStr

	var cached []vision.ScheduleEntry
	if s.store.GetJSON(cacheKey, &cached) {
		log.Printf("Gomos: merge cache hit")
		return cached, nil
	}

	merged, rawResponse, err := s.vision.MergeScheduleEntries(ctx, schedules)
	if err != nil {
		return nil, fmt.Errorf("merging entries: %w", err)
	}

	// Persist structured result
	if data, merr := json.Marshal(merged); merr == nil {
		if werr := s.store.SetRaw(cacheKey+".json", data); werr != nil {
			log.Printf("Gomos: failed to cache merged entries: %v", werr)
		}
	}

	// Persist raw API response
	if werr := s.store.SetRaw(cacheKey+".response.txt", []byte(rawResponse)); werr != nil {
		log.Printf("Gomos: failed to persist merge response: %v", werr)
	}

	return merged, nil
}

// translateEntries translates raw schedule entries to Swedish via the OpenAI API, with caching.
func (s *GomosScraper) translateEntries(ctx context.Context, entries []vision.RawScheduleEntry) ([]vision.ScheduleEntry, error) {
	entriesJSON, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("marshaling entries: %w", err)
	}
	hash := sha256.Sum256(entriesJSON)
	hashStr := hex.EncodeToString(hash[:])
	cacheKey := "translate/" + hashStr

	var cached []vision.ScheduleEntry
	if s.store.GetJSON(cacheKey, &cached) {
		log.Printf("Gomos: translate cache hit")
		return cached, nil
	}

	translated, rawResponse, err := s.vision.TranslateScheduleEntries(ctx, entries)
	if err != nil {
		return nil, fmt.Errorf("translating entries: %w", err)
	}

	// Persist structured result
	if data, merr := json.Marshal(translated); merr == nil {
		if werr := s.store.SetRaw(cacheKey+".json", data); werr != nil {
			log.Printf("Gomos: failed to cache translated entries: %v", werr)
		}
	}

	// Persist raw API response
	if werr := s.store.SetRaw(cacheKey+".response.txt", []byte(rawResponse)); werr != nil {
		log.Printf("Gomos: failed to persist translate response: %v", werr)
	}

	return translated, nil
}

// rawEntriesToSwedish converts RawScheduleEntry to ScheduleEntry directly (no API call needed).
func rawEntriesToSwedish(entries []vision.RawScheduleEntry) []vision.ScheduleEntry {
	result := make([]vision.ScheduleEntry, len(entries))
	for i, e := range entries {
		result[i] = vision.ScheduleEntry{
			Date:        e.Date,
			DayOfWeek:   e.DayOfWeek,
			Time:        e.Time,
			ServiceName: e.ServiceName,
			Occasion:    e.Occasion,
		}
	}
	return result
}

func (s *GomosScraper) findLatestSchedulePost(ctx context.Context) (string, error) {
	doc, err := fetchDocument(ctx, gomosScheduleURL)
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
	doc, err := fetchDocument(ctx, postURL)
	if err != nil {
		return nil, err
	}

	var urls []string
	doc.Find("article img, .entry-content img, .wp-block-image img").Each(func(i int, sel *goquery.Selection) {
		src, exists := sel.Attr("src")
		if !exists {
			return
		}
		// Only include uploaded content images, not theme assets
		if !strings.Contains(src, "/uploads/") {
			return
		}
		if strings.Contains(src, ".jpg") || strings.Contains(src, ".png") || strings.Contains(src, ".jpeg") {
			urls = append(urls, src)
		}
	})

	return urls, nil
}

func (s *GomosScraper) downloadImage(ctx context.Context, imageURL string) ([]byte, error) {
	return fetchURL(ctx, imageURL)
}

func (s *GomosScraper) computeChecksum(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func (s *GomosScraper) imageExtension(url string) string {
	lower := strings.ToLower(url)
	if strings.Contains(lower, ".png") {
		return ".png"
	}
	if strings.Contains(lower, ".jpeg") {
		return ".jpeg"
	}
	return ".jpg"
}

func (s *GomosScraper) convertToServices(entries []vision.ScheduleEntry, sourceURL string) []model.ChurchService {
	var services []model.ChurchService

	for _, entry := range entries {
		location := gomosLocation
		lang := gomosLanguage
		time := entry.Time

		var occasion *string
		if entry.Occasion != "" {
			occasion = &entry.Occasion
		}

		services = append(services, model.ChurchService{
			Parish:      gomosSourceName,
			Source:      gomosSourceName,
			SourceURL:   sourceURL,
			Date:        entry.Date,
			DayOfWeek:   entry.DayOfWeek,
			ServiceName: entry.ServiceName,
			Location:    &location,
			Time:        &time,
			Occasion:    occasion,
			Notes:       nil,
			Language:    &lang,
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
