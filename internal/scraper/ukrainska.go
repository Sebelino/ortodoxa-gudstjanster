package scraper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/store"
	"ortodoxa-gudstjanster/internal/vision"
)

const (
	ukrainskaSourceName  = "Ukrainska ortodoxa kyrkan i Stockholm"
	ukrainskaParishSlug  = "ukrainska-ortodoxa-stockholm"
	ukrainskaScheduleURL = "https://www.ukrortodox.com/rozklad"
	ukrainskaLocation    = "Nynäsvägen 3C, 136 47 Haninge"
)

// UkrainskaScraper scrapes the Ukrainian Orthodox Church's schedule from
// images published on their own website, using OpenAI Vision API to OCR and
// translate them — the same approach as GomosScraper. Previously this
// scraped a Telegram channel instead; switched in October 2026 once the
// parish started publishing schedules on their own site, and after
// Telegram had spent all of September 2026 increasingly rate-limiting the
// ingestion job's requests (failure rate climbed from ~20% to 50-80% of
// runs over the month, affecting the Helige Sergij Telegram scraper too —
// not something retries could fix).
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

	imageURLs, err := s.extractImageURLs(ctx)
	if err != nil {
		return nil, fmt.Errorf("finding schedule images: %w", err)
	}
	s.note("found %d schedule image(s) on the website", len(imageURLs))

	// Each image is OCR'd and converted independently, then merged — the
	// parish sometimes splits a single month's schedule across more than
	// one image by date range (e.g. the 1st-11th in one photo, the 13th-25th
	// in another) rather than reposting the same content twice, so there's
	// no "pick the one true image for this month" step here. Genuine
	// overlaps between images (the same date and time appearing twice) are
	// still caught below by deduplicate, which merges onto the same slot.
	var allServices []model.ChurchService
	imagesRead := 0
	for _, url := range imageURLs {
		data, err := fetchURL(ctx, url)
		if err != nil {
			log.Printf("Ukrainska: failed to download %s: %v", url, err)
			s.note("failed to download %s: %v", url, err)
			continue
		}
		entries, lang, err := s.ocrImage(ctx, data, url)
		if err != nil {
			log.Printf("Ukrainska: OCR failed for %s: %v", url, err)
			s.note("OCR failed for %s: %v", url, err)
			continue
		}
		log.Printf("Ukrainska: %s source, %d entries from %s", lang, len(entries), url)
		allServices = append(allServices, s.convertToServices(entries, ukrainskaScheduleURL)...)
		imagesRead++
	}

	if imagesRead == 0 {
		return nil, fmt.Errorf("no schedule images could be read")
	}

	deduped := s.deduplicate(allServices)
	s.note("extracted %d services (%d after dedup)", len(allServices), len(deduped))
	return deduped, nil
}

// extractImageURLs fetches the parish's schedule page and returns the URLs
// of its embedded schedule images, in document order. The page is a Google
// Sites site; its own logo link is excluded by its "Site home" accessibility
// label — the most stable signal available, since Google Sites' generated
// CSS class names aren't tied to page content and can change on an
// unrelated site rebuild.
func (s *UkrainskaScraper) extractImageURLs(ctx context.Context) ([]string, error) {
	doc, err := fetchDocument(ctx, ukrainskaScheduleURL)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var urls []string
	doc.Find("img[src]").Each(func(_ int, sel *goquery.Selection) {
		src, _ := sel.Attr("src")
		if !strings.Contains(src, "googleusercontent.com/sitesv-images-rt/") {
			return
		}
		if label, _ := sel.Attr("aria-label"); label == "Site home" {
			return
		}
		base := stripGoogleImageSizeParam(src)
		if seen[base] {
			return
		}
		seen[base] = true
		urls = append(urls, src)
	})

	if len(urls) == 0 {
		return nil, fmt.Errorf("no schedule images found on %s", ukrainskaScheduleURL)
	}
	return urls, nil
}

// stripGoogleImageSizeParam removes a Google-hosted image URL's trailing
// "=wNNNN" size parameter, so the same underlying image requested at two
// different sizes is recognized as the same image rather than a duplicate.
func stripGoogleImageSizeParam(url string) string {
	if i := strings.LastIndex(url, "=w"); i != -1 {
		return url[:i]
	}
	return url
}

// ocrImage extracts schedule entries from an image via the Vision API, with
// caching by image checksum, translating to Swedish when the source
// language isn't already Swedish. Returns the translated entries alongside
// the original source language, which is only used for logging.
func (s *UkrainskaScraper) ocrImage(ctx context.Context, imageData []byte, sourceRef string) ([]vision.ScheduleEntry, string, error) {
	checksum := computeChecksum(imageData)
	cacheKey := "ukrainska-ocr/v1/" + checksum

	var raw vision.RawScheduleResult
	if s.store.GetJSON(cacheKey, &raw) {
		log.Printf("Ukrainska: OCR cache hit for %s (checksum %s)", sourceRef, checksum[:12])
	} else {
		log.Printf("Ukrainska: OCR cache miss for %s (checksum %s), calling API", sourceRef, checksum[:12])

		rawPtr, resp, err := s.vision.ExtractScheduleRaw(ctx, imageData)
		if err != nil {
			return nil, "", fmt.Errorf("OCR for %s: %w", sourceRef, err)
		}
		raw = *rawPtr

		if werr := s.store.SetRaw(cacheKey+".response.txt", []byte(resp)); werr != nil {
			log.Printf("Ukrainska: failed to persist OCR response: %v", werr)
		}
		if werr := s.store.SetRaw(cacheKey+".jpg", imageData); werr != nil {
			log.Printf("Ukrainska: failed to persist source image: %v", werr)
		}
		if data, merr := json.Marshal(raw); merr == nil {
			if werr := s.store.SetRaw(cacheKey+".json", data); werr != nil {
				log.Printf("Ukrainska: failed to cache OCR result: %v", werr)
			}
		}
	}

	lang := strings.ToLower(raw.Language)
	if lang == "swedish" || lang == "svenska" {
		return rawToScheduleEntries(raw.Entries), raw.Language, nil
	}

	translated, err := s.translateEntries(ctx, raw.Entries)
	if err != nil {
		return nil, "", fmt.Errorf("translation for %s: %w", sourceRef, err)
	}
	log.Printf("Ukrainska: OCR extracted %d entries (%s) for %s", len(translated), raw.Language, sourceRef)
	return translated, raw.Language, nil
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

// ukrainskaTownPrefixRe matches the Ukrainian "у м." ("in the town of") in front
// of a place name, as written or transliterated.
var ukrainskaTownPrefixRe = regexp.MustCompile(`^(?:[uvув]\s+)?[mм]\.\s*`)

// cleanUkrainskaLocation reduces "u m.Södertälje." to "Södertälje".
func cleanUkrainskaLocation(location string) string {
	return strings.TrimRight(ukrainskaTownPrefixRe.ReplaceAllString(strings.TrimSpace(location), ""), ". ")
}

func (s *UkrainskaScraper) convertToServices(entries []vision.ScheduleEntry, sourceURL string) []model.ChurchService {
	var services []model.ChurchService
	for _, entry := range entries {
		location := ukrainskaLocation
		if entry.Location != "" {
			if entry.Abroad {
				continue
			}
			location = cleanUkrainskaLocation(entry.Location)
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

// deduplicate collapses services onto the same (date, time) slot. Different
// images can describe the same service with different levels of detail
// (e.g. a terse reminder vs. a fuller monthly schedule), so slots are merged
// by date+time rather than by exact name text, keeping the more detailed of
// the two descriptions.
func (s *UkrainskaScraper) deduplicate(services []model.ChurchService) []model.ChurchService {
	if len(services) == 0 {
		return services
	}

	best := make(map[string]model.ChurchService)
	var order []string

	for _, svc := range services {
		timeStr := ""
		if svc.Time != nil {
			timeStr = *svc.Time
		}
		key := fmt.Sprintf("%s|%s", svc.Date, timeStr)

		existing, ok := best[key]
		if !ok {
			best[key] = svc
			order = append(order, key)
			continue
		}
		// Prefer the more detailed name; break an exact-length tie on
		// SourceURL so the winner doesn't depend on input order.
		switch {
		case len(svc.ServiceName) > len(existing.ServiceName):
			best[key] = svc
		case len(svc.ServiceName) == len(existing.ServiceName) && svc.SourceURL < existing.SourceURL:
			best[key] = svc
		}
	}

	sort.Strings(order)
	result := make([]model.ChurchService, 0, len(order))
	for _, key := range order {
		result = append(result, best[key])
	}

	return result
}
