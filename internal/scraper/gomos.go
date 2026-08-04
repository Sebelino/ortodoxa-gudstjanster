package scraper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/store"
	"ortodoxa-gudstjanster/internal/vision"
)

const (
	gomosSourceName  = "St. Georgios Cathedral"
	gomosParishSlug  = "st-georgios"
	gomosScheduleURL = "https://gomos.se/en/category/schedule/"
	gomosLocation    = "Birger Jarlsgatan 92, 114 20 Stockholm"
)

// GomosScraper scrapes the St. Georgios Cathedral schedule using OpenAI Vision API.
type GomosScraper struct {
	NoteCollector
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
	s.resetNotes()

	// Collect images from all sources
	var allImages []imageWithData

	websiteImages, websiteErr := s.fetchWebsiteImages(ctx)
	if websiteErr != nil {
		log.Printf("Gomos: website failed: %v", websiteErr)
		s.note("website fetch failed: %v", websiteErr)
	} else {
		s.note("website: fetched %d image(s)", len(websiteImages))
	}
	allImages = append(allImages, websiteImages...)

	if s.uploadReader != nil {
		bucketImages, bucketErr := s.fetchBucketImages(ctx)
		if bucketErr != nil {
			log.Printf("Gomos: bucket failed: %v", bucketErr)
			s.note("GCS upload bucket fetch failed: %v", bucketErr)
		} else {
			s.note("GCS upload bucket: fetched %d image(s)", len(bucketImages))
		}
		allImages = append(allImages, bucketImages...)
	}

	if len(allImages) == 0 {
		if websiteErr != nil {
			return nil, websiteErr
		}
		return nil, fmt.Errorf("no images found")
	}

	// Process all images together: OCR, deduplicate by month, convert
	services, err := s.processImages(ctx, allImages)
	if err != nil {
		return nil, err
	}

	deduped := s.deduplicate(services)

	// If the website failed and all resulting events are past-dated, the backup
	// data is stale. Return an error so the existing scraper-failure alert fires.
	if websiteErr != nil && len(deduped) > 0 {
		today := time.Now().Format("2006-01-02")
		futureCount := 0
		for _, svc := range deduped {
			if svc.Date >= today {
				futureCount++
			}
		}
		if futureCount == 0 {
			return nil, fmt.Errorf("website failed (%v) and backup data is stale (%d events, all past-dated)", websiteErr, len(deduped))
		}
	}

	return deduped, nil
}

// imageWithData pairs downloaded image bytes with source metadata.
type imageWithData struct {
	data      []byte
	sourceRef string // URL or bucket object name
	sourceURL string // the URL to use as source in the service
}

// ocrResult pairs OCR-extracted Swedish entries with source metadata.
type ocrResult struct {
	language  string
	entries   []vision.ScheduleEntry
	sourceURL string
}

// ocrCacheEntry is returned by ocrImage: language of the source image plus
// entries already translated to Swedish. The cache (gomos-ocr/v3/) stores the
// raw OCR result (vision.RawScheduleResult) so that translation can be
// invalidated and re-run independently by clearing the translate cache.
type ocrCacheEntry struct {
	Language string                 `json:"language"`
	Entries  []vision.ScheduleEntry `json:"entries"`
}

// processImages is the core pipeline: OCR each image to Swedish entries,
// group by month, prefer Swedish source for same-month duplicates, convert.
func (s *GomosScraper) processImages(ctx context.Context, images []imageWithData) ([]model.ChurchService, error) {

	// Step 1: OCR each image → Swedish ScheduleEntry slice
	var results []ocrResult
	for _, img := range images {
		res, err := s.ocrImage(ctx, img.data, img.sourceRef)
		if err != nil {
			log.Printf("Gomos: OCR failed for %s: %v", img.sourceRef, err)
			s.note("OCR failed for %s: %v", img.sourceRef, err)
			continue
		}
		results = append(results, ocrResult{
			language:  res.Language,
			entries:   res.Entries,
			sourceURL: img.sourceURL,
		})
	}

	// Step 2: Group by month
	type group struct {
		items []ocrResult
	}
	groups := make(map[string]*group)
	var order []string
	for _, r := range results {
		month := scheduleMonthFromEntries(r.entries)
		if _, ok := groups[month]; !ok {
			groups[month] = &group{}
			order = append(order, month)
		}
		groups[month].items = append(groups[month].items, r)
	}

	// Step 3: For each month group, prefer Swedish > English > other (Greek fallback).
	// Among same-priority candidates, prefer the one with more entries — a partial
	// OCR read of one photo should not silently win over a complete read of another.
	var allServices []model.ChurchService
	for _, month := range order {
		g := groups[month]

		chosen := g.items[0]
		chosenPriority := langPriority(chosen.language)
		for _, item := range g.items[1:] {
			p := langPriority(item.language)
			if p < chosenPriority || (p == chosenPriority && len(item.entries) > len(chosen.entries)) {
				chosen = item
				chosenPriority = p
			}
		}

		log.Printf("Gomos: using %s source for %s (%d entries)", chosen.language, month, len(chosen.entries))
		allServices = append(allServices, s.convertToServices(chosen.entries, chosen.sourceURL)...)
	}

	return allServices, nil
}

// ocrImage extracts schedule entries from an image, returning Swedish entries.
// The raw OCR result is cached by image checksum under gomos-ocr/v3/ as a
// vision.RawScheduleResult. Translation is always done via translateEntries,
// which has its own cache (translate/v2/). This separation means translations
// can be re-run by clearing only the translate cache, without re-running OCR.
func (s *GomosScraper) ocrImage(ctx context.Context, imageData []byte, sourceRef string) (*ocrCacheEntry, error) {
	checksum := s.computeChecksum(imageData)
	cacheKey := "gomos-ocr/v3/" + checksum

	// Check OCR cache for raw (untranslated) result.
	var raw vision.RawScheduleResult
	if s.store.GetJSON(cacheKey, &raw) {
		log.Printf("Gomos: OCR cache hit for %s (checksum %s)", sourceRef, checksum[:12])
	} else {
		log.Printf("Gomos: OCR cache miss for %s (checksum %s), calling API", sourceRef, checksum[:12])

		var rawResponse string
		var err error
		rawPtr, resp, err := s.vision.ExtractScheduleRaw(ctx, imageData)
		if err != nil {
			return nil, fmt.Errorf("OCR for %s: %w", sourceRef, err)
		}
		raw = *rawPtr
		rawResponse = resp

		// Persist raw API response for diagnostics
		if werr := s.store.SetRaw(cacheKey+".response.txt", []byte(rawResponse)); werr != nil {
			log.Printf("Gomos: failed to persist OCR response: %v", werr)
		}

		// Persist source image
		imageExt := s.imageExtension(sourceRef)
		if werr := s.store.SetRaw(cacheKey+imageExt, imageData); werr != nil {
			log.Printf("Gomos: failed to persist source image: %v", werr)
		}

		// Cache raw result so future runs skip the expensive OCR API call.
		if data, merr := json.Marshal(raw); merr == nil {
			if werr := s.store.SetRaw(cacheKey+".json", data); werr != nil {
				log.Printf("Gomos: failed to cache OCR result: %v", werr)
			}
		}
	}

	// Translate to Swedish — always via translateEntries so the translate cache
	// can be cleared independently to re-run translation with an updated prompt.
	var entries []vision.ScheduleEntry
	lang := strings.ToLower(raw.Language)
	if lang == "swedish" || lang == "svenska" {
		entries = rawEntriesToSwedish(raw.Entries)
	} else {
		var err error
		entries, err = s.translateEntries(ctx, raw.Entries)
		if err != nil {
			return nil, fmt.Errorf("translation for %s: %w", sourceRef, err)
		}
	}

	log.Printf("Gomos: OCR extracted %d entries (%s) for %s", len(entries), raw.Language, sourceRef)
	return &ocrCacheEntry{
		Language: raw.Language,
		Entries:  entries,
	}, nil
}

// scheduleMonthFromEntries returns the most common year-month (YYYY-MM) among entries.
func scheduleMonthFromEntries(entries []vision.ScheduleEntry) string {
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

// translateEntries translates raw schedule entries to Swedish via the OpenAI API, with caching.
func (s *GomosScraper) translateEntries(ctx context.Context, entries []vision.RawScheduleEntry) ([]vision.ScheduleEntry, error) {
	entriesJSON, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("marshaling entries: %w", err)
	}
	hash := sha256.Sum256(entriesJSON)
	hashStr := hex.EncodeToString(hash[:])
	cacheKey := "translate/v2/" + hashStr

	var cached []vision.ScheduleEntry
	if s.store.GetJSON(cacheKey, &cached) {
		log.Printf("Gomos: translate cache hit")
		fixSundayOrdinals(cached)
		fixArchbishopTitle(cached)
		return cached, nil
	}

	translated, rawResponse, err := s.vision.TranslateScheduleEntries(ctx, entries)
	if err != nil {
		return nil, fmt.Errorf("translating entries: %w", err)
	}

	fixSundayOrdinals(translated)
	fixArchbishopTitle(translated)

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

var swedishSundayOrdinals = []string{
	"Första", "Andra", "Tredje", "Fjärde", "Femte", "Sjätte", "Sjunde", "Åttonde", "Nionde", "Tionde",
	"Elfte", "Tolfte", "Trettonde", "Fjortonde", "Femtonde", "Sextonde", "Sjuttonde", "Artonde", "Nittonde", "Tjugonde",
}

var sundayOrdinalRe = regexp.MustCompile(`^(\p{Lu}\p{Ll}+) söndagen i (Matteus|Lukas)$`)

func sundayOrdinalNumber(word string) int {
	for i, w := range swedishSundayOrdinals {
		if strings.EqualFold(w, word) {
			return i + 1
		}
	}
	return 0
}

func sundayOrdinalWord(n int) string {
	if n < 1 || n > len(swedishSundayOrdinals) {
		return ""
	}
	return swedishSundayOrdinals[n-1]
}

// fixSundayOrdinals recomputes "[ordinal] söndagen i [evangelist]" occasions using
// the earliest dated entry in each evangelist group as a trusted anchor, and calendar
// week arithmetic (always exact, since consecutive Sundays are exactly 7 days apart)
// for the rest. OCR/translation occasionally misreads Greek compound numerals (e.g.
// ΙΓ' as ΙΒ'), which trusting the AI's own numeral transcription can't reliably catch —
// dates are read far more reliably than these fine numeral suffixes, so we prefer them.
func fixSundayOrdinals(entries []vision.ScheduleEntry) {
	type anchor struct {
		date time.Time
		num  int
	}
	anchors := make(map[string]anchor)
	for _, e := range entries {
		m := sundayOrdinalRe.FindStringSubmatch(e.Occasion)
		if m == nil {
			continue
		}
		num := sundayOrdinalNumber(m[1])
		if num == 0 {
			continue
		}
		d, err := time.Parse("2006-01-02", e.Date)
		if err != nil {
			continue
		}
		evangelist := m[2]
		if a, ok := anchors[evangelist]; !ok || d.Before(a.date) {
			anchors[evangelist] = anchor{date: d, num: num}
		}
	}

	for i := range entries {
		m := sundayOrdinalRe.FindStringSubmatch(entries[i].Occasion)
		if m == nil {
			continue
		}
		evangelist := m[2]
		a := anchors[evangelist]
		d, err := time.Parse("2006-01-02", entries[i].Date)
		if err != nil {
			continue
		}
		weeksApart := int(d.Sub(a.date).Hours()) / (24 * 7)
		word := sundayOrdinalWord(a.num + weeksApart)
		if word == "" {
			continue
		}
		corrected := word + " söndagen i " + evangelist
		if corrected != entries[i].Occasion {
			log.Printf("Gomos: correcting Sunday ordinal %q -> %q for %s", entries[i].Occasion, corrected, entries[i].Date)
			entries[i].Occasion = corrected
		}
	}
}

// Cleopas is the Archbishop of Sweden. Across translation runs from April to August 2026,
// the model has repeatedly mistransliterated his name as "Kleopas" (the natural Greek→Swedish
// K-transliteration) regardless of what the prompt says at the time — it has done this even
// when the prompt's own examples spelled it correctly. The regex only matches when the
// captured name is already a recognized variant of his name, so it corrects known
// mistransliterations and honorific-class slips (e.g. "Nåd" instead of "Eminens Ärkebiskop")
// without blindly overwriting a name it hasn't verified.
var cleopasHonorificRe = regexp.MustCompile(`Hans (?:Eminens|Nåd|Högvördighet)(?: Ärkebiskop)? (?:Cleopas|Kleopas) av Sverige`)

// fixArchbishopTitle corrects known mistransliterations and honorific-class errors for
// Archbishop Cleopas and Bishop Bartholomaios — both known, fixed facts that the translation
// model occasionally garbles.
func fixArchbishopTitle(entries []vision.ScheduleEntry) {
	for i := range entries {
		entries[i].ServiceName = cleopasHonorificRe.ReplaceAllString(entries[i].ServiceName, "Hans Eminens Ärkebiskop Cleopas av Sverige")
		entries[i].ServiceName = bartholomaiosHonorificRe.ReplaceAllString(entries[i].ServiceName, "Hans Nåd Bartholomaios av Elaia")
	}
}

// Bartholomaios is the (titular) Bishop of Elaia. Symmetric with Cleopas above: only
// recognized name variants (including the Latinized forms the prompt already warns
// against) get corrected, not any arbitrary name styled "av Elaia".
var bartholomaiosHonorificRe = regexp.MustCompile(`Hans (?:Eminens|Nåd|Högvördighet)(?: Biskop| Ärkebiskop)? (?:Bartholomaios|Bartholomeus|Bartholomew) av Elaia`)

// langPriority returns a priority for the given language string (lower = preferred).
// Swedish = 0, English = 1, anything else (e.g. Greek) = 2.
func langPriority(lang string) int {
	switch strings.ToLower(lang) {
	case "swedish", "svenska":
		return 0
	case "english", "engelska":
		return 1
	default:
		return 2
	}
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
			Location:    e.Location,
			Abroad:      e.Abroad,
		}
	}
	return result
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
			// WordPress sets src to a resized display copy and lists the full
			// range of sizes (including the full-resolution original) in
			// srcset. A compressed/resized copy is more likely to lose fine
			// detail — like the numeral suffixes on Greek ordinals — under
			// OCR, so prefer the largest available variant when present.
			if best := largestSrcsetURL(sel); best != "" {
				src = best
			}
			urls = append(urls, src)
		}
	})

	return urls, nil
}

// largestSrcsetURL returns the URL of the widest image in an img tag's srcset
// attribute (format: "url1 100w, url2 200w, ..."), or "" if absent/unparseable.
func largestSrcsetURL(sel *goquery.Selection) string {
	srcset, exists := sel.Attr("srcset")
	if !exists {
		return ""
	}

	var bestURL string
	bestWidth := -1
	for _, candidate := range strings.Split(srcset, ",") {
		fields := strings.Fields(strings.TrimSpace(candidate))
		if len(fields) != 2 || !strings.HasSuffix(fields[1], "w") {
			continue
		}
		width, err := strconv.Atoi(strings.TrimSuffix(fields[1], "w"))
		if err != nil {
			continue
		}
		if width > bestWidth {
			bestWidth = width
			bestURL = fields[0]
		}
	}
	return bestURL
}

func (s *GomosScraper) downloadImage(ctx context.Context, imageURL string) ([]byte, error) {
	return fetchURL(ctx, imageURL)
}

func (s *GomosScraper) computeChecksum(data []byte) string {
	return computeChecksum(data)
}

func (s *GomosScraper) imageExtension(url string) string {
	lower := strings.ToLower(url)
	if strings.HasSuffix(lower, ".png") {
		return ".png"
	}
	if strings.HasSuffix(lower, ".jpeg") {
		return ".jpeg"
	}
	return ".jpg"
}

func (s *GomosScraper) convertToServices(entries []vision.ScheduleEntry, sourceURL string) []model.ChurchService {
	var services []model.ChurchService

	for _, entry := range entries {
		if strings.EqualFold(strings.TrimSpace(entry.ServiceName), "archeirinon") {
			continue
		}

		location := gomosLocation
		if entry.Location != "" {
			if entry.Abroad {
				continue
			}
			location = entry.Location
		}
		time := entry.Time

		var occasion *string
		if entry.Occasion != "" {
			occasion = &entry.Occasion
		}

		services = append(services, model.ChurchService{
			Parish:      "",
			ParishSlug:  gomosParishSlug,
			Source:      gomosSourceName,
			SourceURL:   sourceURL,
			Date:        entry.Date,
			DayOfWeek:   entry.DayOfWeek,
			ServiceName: entry.ServiceName,
			Location:  &location,
			Time:      &time,
			Occasion:  occasion,
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
