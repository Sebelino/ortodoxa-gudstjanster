package scraper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/store"
	"ortodoxa-gudstjanster/internal/vision"
)

const (
	ukrainskaSourceName = "Ukrainska ortodoxa kyrkan i Stockholm"
	ukrainskaParishSlug = "ukrainska-ortodoxa-stockholm"
	ukrainskaChannelURL = "https://t.me/ukrcerkva_stockholm"
	ukrainskaLocation   = "Nynäsvägen 3C, 136 47 Haninge"
	// Number of recent posts to scan for schedule images.
	ukrainskaScanCount = 100
)

// UkrainskaScraper scrapes the Ukrainian Orthodox Church schedule from their
// Telegram channel using OpenAI Vision API to OCR schedule images.
type UkrainskaScraper struct {
	NoteCollector
	store  store.Store
	vision *vision.Client
}

// NewUkrainskaScraper creates a new scraper for the Ukrainian Orthodox Church in Stockholm.
func NewUkrainskaScraper(s store.Store, v *vision.Client) *UkrainskaScraper {
	return &UkrainskaScraper{
		store:  s,
		vision: v,
	}
}

func (s *UkrainskaScraper) Name() string {
	return ukrainskaSourceName
}

func (s *UkrainskaScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	s.resetNotes()

	// Step 1: Find the latest post number via binary search.
	latestPost, err := s.findLatestPost(ctx)
	if err != nil {
		return nil, fmt.Errorf("finding latest post: %w", err)
	}
	s.note("latest post: %d", latestPost)

	// Step 2: Scan recent posts for schedule images.
	images, sourceURL, err := s.findScheduleImages(ctx, latestPost)
	if err != nil {
		return nil, fmt.Errorf("finding schedule images: %w", err)
	}
	s.note("found %d schedule image(s)", len(images))

	if len(images) == 0 {
		return nil, fmt.Errorf("no schedule images found in recent posts")
	}

	// Step 3: OCR each image and collect services.
	var allServices []model.ChurchService
	for i, img := range images {
		entries, err := s.ocrImage(ctx, img, fmt.Sprintf("image-%d", i))
		if err != nil {
			log.Printf("Ukrainska: OCR failed for image %d: %v", i, err)
			s.note("OCR failed for image %d: %v", i, err)
			continue
		}
		services := s.convertToServices(entries, sourceURL)
		allServices = append(allServices, services...)
	}

	if len(allServices) == 0 {
		return nil, fmt.Errorf("no services extracted from schedule images")
	}

	deduped := s.deduplicate(allServices)
	s.note("extracted %d services (%d after dedup)", len(allServices), len(deduped))
	return deduped, nil
}

// findLatestPost finds the latest post number in the channel.
//
// Telegram channels can have large gaps in post numbering (deleted messages),
// so a naive binary search fails. Instead we probe exponentially to find a
// confirmed-valid post, then scan forward in chunks to find the frontier.
func (s *UkrainskaScraper) findLatestPost(ctx context.Context) (int, error) {
	isValid := func(n int) (bool, error) {
		url := fmt.Sprintf("%s/%d?embed=1", ukrainskaChannelURL, n)
		data, err := fetchURL(ctx, url)
		if err != nil {
			return false, err
		}
		return !strings.Contains(string(data), "tgme_widget_message_error"), nil
	}

	// anyValidInRange checks if at least one post exists in [lo, hi].
	anyValidInRange := func(lo, hi int) (int, bool, error) {
		// Sample up to 10 evenly-spaced posts in the range.
		span := hi - lo + 1
		step := span / 10
		if step < 1 {
			step = 1
		}
		for n := lo; n <= hi; n += step {
			valid, err := isValid(n)
			if err != nil {
				return 0, false, err
			}
			if valid {
				return n, true, nil
			}
		}
		return 0, false, nil
	}

	// Phase 1: Find a valid post by probing exponentially.
	probe := 100
	lastValid := 0
	for {
		valid, err := isValid(probe)
		if err != nil {
			return 0, fmt.Errorf("probing post %d: %w", probe, err)
		}
		if valid {
			lastValid = probe
			probe *= 2
			continue
		}
		// The probe failed — but there might be valid posts beyond a gap.
		// Check if any post exists in [lastValid+1, probe].
		if lastValid > 0 {
			found, ok, err := anyValidInRange(lastValid+1, probe)
			if err != nil {
				return 0, err
			}
			if ok {
				lastValid = found
				probe = found * 2
				continue
			}
		}
		break
	}

	if lastValid == 0 {
		return 0, fmt.Errorf("no valid posts found")
	}

	// Phase 2: From lastValid, scan forward in chunks of 100 to find the frontier.
	// Keep going as long as we find at least one valid post in each chunk.
	frontier := lastValid
	for {
		chunkStart := frontier + 1
		chunkEnd := frontier + 100
		_, ok, err := anyValidInRange(chunkStart, chunkEnd)
		if err != nil {
			return 0, err
		}
		if !ok {
			break
		}
		// Find the highest valid post in this chunk by scanning backwards.
		for n := chunkEnd; n >= chunkStart; n-- {
			valid, err := isValid(n)
			if err != nil {
				return 0, err
			}
			if valid {
				frontier = n
				break
			}
		}
	}

	return frontier, nil
}

// telegramPost holds parsed data from a Telegram embed page.
type telegramPost struct {
	number    int
	text      string
	imageURLs []string
	datetime  string
}

var (
	tgImageRe   = regexp.MustCompile(`background-image:url\('(https://cdn[^']+)'\)`)
	tgDateRe    = regexp.MustCompile(`datetime="([^"]+)"`)
)

// fetchPost fetches and parses a single Telegram channel post embed.
func (s *UkrainskaScraper) fetchPost(ctx context.Context, postNum int) (*telegramPost, error) {
	url := fmt.Sprintf("%s/%d?embed=1", ukrainskaChannelURL, postNum)
	doc, err := fetchDocument(ctx, url)
	if err != nil {
		return nil, err
	}

	// Check if this is an error page.
	if doc.Find(".tgme_widget_message_error").Length() > 0 {
		return nil, fmt.Errorf("post %d does not exist", postNum)
	}

	post := &telegramPost{number: postNum}

	// Extract text content.
	doc.Find(".js-message_text").First().Each(func(_ int, sel *goquery.Selection) {
		post.text = sel.Text()
	})

	// Extract image URLs from background-image styles.
	html, _ := doc.Html()
	for _, match := range tgImageRe.FindAllStringSubmatch(html, -1) {
		post.imageURLs = append(post.imageURLs, match[1])
	}

	// Extract datetime.
	if match := tgDateRe.FindStringSubmatch(html); len(match) > 1 {
		post.datetime = match[1]
	}

	return post, nil
}

// findScheduleImages scans the most recent posts for schedule announcements
// and returns the images from the most recent schedule post.
func (s *UkrainskaScraper) findScheduleImages(ctx context.Context, latestPost int) ([][]byte, string, error) {
	start := latestPost
	end := latestPost - ukrainskaScanCount
	if end < 1 {
		end = 1
	}

	for postNum := start; postNum >= end; postNum-- {
		post, err := s.fetchPost(ctx, postNum)
		if err != nil {
			continue
		}

		// Look for schedule posts: "розклад богослужінь" (schedule of services)
		if !strings.Contains(strings.ToLower(post.text), "розклад") {
			continue
		}

		if len(post.imageURLs) == 0 {
			continue
		}

		log.Printf("Ukrainska: found schedule post %d with %d image(s): %s",
			postNum, len(post.imageURLs), post.text[:min(80, len(post.text))])
		s.note("schedule post %d (%s): %d image(s)", postNum, post.datetime, len(post.imageURLs))

		sourceURL := fmt.Sprintf("%s/%d", ukrainskaChannelURL, postNum)

		var images [][]byte
		for _, imgURL := range post.imageURLs {
			data, err := fetchURL(ctx, imgURL)
			if err != nil {
				log.Printf("Ukrainska: failed to download image: %v", err)
				s.note("image download failed: %v", err)
				continue
			}
			images = append(images, data)
		}

		if len(images) > 0 {
			return images, sourceURL, nil
		}
	}

	return nil, "", fmt.Errorf("no schedule post found in last %d posts", ukrainskaScanCount)
}

// ocrImage extracts schedule entries from an image using the Vision API, with caching.
func (s *UkrainskaScraper) ocrImage(ctx context.Context, imageData []byte, sourceRef string) ([]vision.ScheduleEntry, error) {
	checksum := computeChecksum(imageData)
	cacheKey := "ukrainska-ocr/v1/" + checksum

	// Check OCR cache.
	var raw vision.RawScheduleResult
	if s.store.GetJSON(cacheKey, &raw) {
		log.Printf("Ukrainska: OCR cache hit for %s (checksum %s)", sourceRef, checksum[:12])
	} else {
		log.Printf("Ukrainska: OCR cache miss for %s (checksum %s), calling API", sourceRef, checksum[:12])

		rawPtr, resp, err := s.vision.ExtractScheduleRaw(ctx, imageData)
		if err != nil {
			return nil, fmt.Errorf("OCR for %s: %w", sourceRef, err)
		}
		raw = *rawPtr

		// Persist raw API response for diagnostics.
		if werr := s.store.SetRaw(cacheKey+".response.txt", []byte(resp)); werr != nil {
			log.Printf("Ukrainska: failed to persist OCR response: %v", werr)
		}

		// Persist source image.
		if werr := s.store.SetRaw(cacheKey+".jpg", imageData); werr != nil {
			log.Printf("Ukrainska: failed to persist source image: %v", werr)
		}

		// Cache raw result.
		if data, merr := json.Marshal(raw); merr == nil {
			if werr := s.store.SetRaw(cacheKey+".json", data); werr != nil {
				log.Printf("Ukrainska: failed to cache OCR result: %v", werr)
			}
		}
	}

	// Translate to Swedish if needed.
	lang := strings.ToLower(raw.Language)
	if lang == "swedish" || lang == "svenska" {
		return rawToScheduleEntries(raw.Entries), nil
	}

	translated, err := s.translateEntries(ctx, raw.Entries)
	if err != nil {
		return nil, fmt.Errorf("translation for %s: %w", sourceRef, err)
	}

	log.Printf("Ukrainska: OCR extracted %d entries (%s) for %s", len(translated), raw.Language, sourceRef)
	return translated, nil
}

// translateEntries translates raw schedule entries to Swedish, with caching.
func (s *UkrainskaScraper) translateEntries(ctx context.Context, entries []vision.RawScheduleEntry) ([]vision.ScheduleEntry, error) {
	entriesJSON, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("marshaling entries: %w", err)
	}
	hash := sha256.Sum256(entriesJSON)
	hashStr := hex.EncodeToString(hash[:])
	cacheKey := "ukrainska-translate/v1/" + hashStr

	var cached []vision.ScheduleEntry
	if s.store.GetJSON(cacheKey, &cached) {
		log.Printf("Ukrainska: translate cache hit")
		return cached, nil
	}

	translated, rawResponse, err := s.vision.TranslateScheduleEntries(ctx, entries)
	if err != nil {
		return nil, fmt.Errorf("translating entries: %w", err)
	}

	// Persist structured result.
	if data, merr := json.Marshal(translated); merr == nil {
		if werr := s.store.SetRaw(cacheKey+".json", data); werr != nil {
			log.Printf("Ukrainska: failed to cache translated entries: %v", werr)
		}
	}

	// Persist raw API response.
	if werr := s.store.SetRaw(cacheKey+".response.txt", []byte(rawResponse)); werr != nil {
		log.Printf("Ukrainska: failed to persist translate response: %v", werr)
	}

	return translated, nil
}

// rawToScheduleEntries converts RawScheduleEntry to ScheduleEntry (no translation needed).
func rawToScheduleEntries(entries []vision.RawScheduleEntry) []vision.ScheduleEntry {
	result := make([]vision.ScheduleEntry, len(entries))
	for i, e := range entries {
		result[i] = vision.ScheduleEntry{
			Date:        e.Date,
			DayOfWeek:   e.DayOfWeek,
			Time:        e.Time,
			ServiceName: e.ServiceName,
			Occasion:    e.Occasion,
			Location:    e.Location,
			Abroad:      e.Abroad,
		}
	}
	return result
}

func (s *UkrainskaScraper) convertToServices(entries []vision.ScheduleEntry, sourceURL string) []model.ChurchService {
	var services []model.ChurchService
	for _, entry := range entries {
		location := ukrainskaLocation
		if entry.Location != "" {
			if entry.Abroad {
				continue
			}
			location = entry.Location
		}
		t := entry.Time

		var occasion *string
		if entry.Occasion != "" {
			occasion = &entry.Occasion
		}

		services = append(services, model.ChurchService{
			Parish:      "",
			ParishSlug:  ukrainskaParishSlug,
			Source:      ukrainskaSourceName,
			SourceURL:   sourceURL,
			Date:        entry.Date,
			DayOfWeek:   entry.DayOfWeek,
			ServiceName: entry.ServiceName,
			Location:    &location,
			Time:        &t,
			Occasion:    occasion,
		})
	}
	return services
}

func (s *UkrainskaScraper) deduplicate(services []model.ChurchService) []model.ChurchService {
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
