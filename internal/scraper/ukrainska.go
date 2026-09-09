package scraper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

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
	// Trailing window used only to seed scanning the very first time this
	// scraper runs (no persisted state yet), or if persisted state is ever
	// lost and it cold-starts again. After that, scanning is incremental —
	// see findScheduleImages. A schedule is sometimes posted as a multi-photo
	// album whose messages then sit unchanged for weeks while individual
	// reminder reposts accumulate after it — 100 was once narrowly too
	// short to reach back to such an album (see the 12210-12212 backfill,
	// September 2026), silently and permanently missing it since the
	// watermark only ever moves forward from wherever cold start lands.
	// Widened with a generous margin against that recurring on any future
	// cold start; the extra cost is one-time; incremental runs are
	// unaffected.
	ukrainskaScanCount = 400
	// Safety cap on how far back a single run will catch up if ingestion
	// hasn't run in a long time, so a large gap in scan history can't turn
	// into one run fetching thousands of posts.
	ukrainskaMaxCatchUpPosts = 1000
	// Persisted scan state cache key (see ukrainskaScanState).
	ukrainskaScanStateKey = "ukrainska/scan-state/v1"
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

	state := s.loadScanState()

	// Step 1: Find the latest post number. Seeded from the last scan's
	// frontier when we have one, so this is normally a cheap check of a
	// post we already know is close to current, rather than a blind
	// exponential search starting at a fixed post number every run.
	// Telegram's embed pages intermittently serve a transient "not found"
	// response for a post that does in fact exist — retried a few seconds
	// later, it normally succeeds, so a single blip shouldn't fail the
	// whole run (or repeat as a daily alert for a problem that isn't
	// actually ongoing).
	var latestPost int
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		latestPost, err = s.findLatestPost(ctx, state.HighestScannedPost)
		if err == nil {
			break
		}
		s.note("attempt %d/3: finding latest post failed: %v", attempt, err)
		if attempt < 3 {
			log.Printf("Ukrainska: finding latest post failed on attempt %d/3, retrying in 10s: %v", attempt, err)
			time.Sleep(10 * time.Second)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("finding latest post: %w", err)
	}
	log.Printf("Ukrainska: latest post: %d", latestPost)
	s.note("latest post: %d", latestPost)

	// Step 2: Scan for schedule images. Only posts newer than the last scan
	// are actually fetched; previously found schedule images are carried
	// forward from persisted state until their own dates are in the past —
	// so a schedule doesn't silently disappear just because newer,
	// unrelated posts have pushed it past a fixed trailing window.
	images, err := s.findScheduleImages(ctx, latestPost, &state)
	// Save scan progress even on failure below — findScheduleImages may
	// have already recorded newly-checked posts (schedule or not) in
	// state, and losing that would mean re-fetching them all again next
	// run for no benefit.
	if saveErr := s.saveScanState(state); saveErr != nil {
		log.Printf("Ukrainska: failed to save scan state: %v", saveErr)
	}
	if err != nil {
		return nil, fmt.Errorf("finding schedule images: %w", err)
	}

	// Step 3: OCR each image and collect services. A schedule image with no
	// remaining future dates is still returned here (harmless — the
	// ingestion pipeline's own count-decrease guard already handles that
	// gracefully) rather than dropped from state immediately: state is
	// pruned by age instead (see pruneOldPosts below), not by whether an
	// image currently has future dates, so a run is never left with
	// nothing to report just because the one schedule known about happens
	// to have run its course since the last update.
	var allServices []model.ChurchService
	for i, img := range images {
		entries, err := s.ocrImage(ctx, img, fmt.Sprintf("image-%d", i))
		if err != nil {
			log.Printf("Ukrainska: OCR failed for image %d: %v", i, err)
			s.note("OCR failed for image %d: %v", i, err)
			continue
		}
		services := s.convertToServices(entries, img.sourceURL)
		allServices = append(allServices, services...)
	}

	prunedCount := pruneOldPosts(&state)
	if prunedCount > 0 {
		s.note("dropped %d post record(s) older than the retention window", prunedCount)
	}
	if err := s.saveScanState(state); err != nil {
		log.Printf("Ukrainska: failed to save scan state: %v", err)
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
//
// hint, when nonzero, is the highest post number confirmed valid on a
// previous run. If it's still valid — which it almost always is, since
// posts are rarely deleted — Phase 1 below is skipped entirely and we jump
// straight to Phase 2's forward scan from there, normally making this a
// single request instead of a full search from scratch. Doubling up from a
// stale hint instead (rather than skipping straight to Phase 2) would risk
// overshooting far past the real frontier, so a hint that doesn't check out
// is discarded rather than used as a starting point for that search.
func (s *UkrainskaScraper) findLatestPost(ctx context.Context, hint int) (int, error) {
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

	lastValid := 0
	if hint > 0 {
		if valid, err := isValid(hint); err == nil && valid {
			lastValid = hint
		}
	}

	if lastValid == 0 {
		// Phase 1: Find a valid post by probing exponentially.
		probe := 100
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
			// The probe failed — either a genuine gap, or the specific post
			// we hit is transiently unavailable (Telegram intermittently
			// serves an error page for a post that does in fact exist).
			// Either way, check for any valid post in [lastValid+1, probe]
			// before giving up; this also covers the very first probe
			// (lastValid still 0), which previously had no fallback and
			// made the whole lookup fail whenever post 100 specifically
			// didn't respond as valid.
			lo := lastValid + 1
			found, ok, err := anyValidInRange(lo, probe)
			if err != nil {
				return 0, err
			}
			if ok {
				lastValid = found
				probe = found * 2
				continue
			}
			break
		}
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

// errPostNotFound marks a post as confirmed deleted/never existed (Telegram
// served its error-marker page), as opposed to a transient fetch failure.
// Only posts confirmed this way are permanently retired from future scans —
// see findScheduleImages.
var errPostNotFound = errors.New("post does not exist")

// telegramPost holds parsed data from a Telegram embed page.
type telegramPost struct {
	number    int
	text      string
	imageURLs []string
	datetime  string
}

var (
	tgImageRe = regexp.MustCompile(`background-image:url\('(https://cdn[^']+)'\)`)
	tgDateRe  = regexp.MustCompile(`datetime="([^"]+)"`)
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
		return nil, fmt.Errorf("post %d: %w", postNum, errPostNotFound)
	}

	post := &telegramPost{number: postNum}

	// Extract text content.
	doc.Find(".js-message_text").First().Each(func(_ int, sel *goquery.Selection) {
		post.text = sel.Text()
	})

	// Extract image URLs from background-image styles on photo elements.
	doc.Find("[style]").Each(func(_ int, sel *goquery.Selection) {
		style, exists := sel.Attr("style")
		if !exists {
			return
		}
		for _, match := range tgImageRe.FindAllStringSubmatch(style, -1) {
			post.imageURLs = append(post.imageURLs, match[1])
		}
	})

	// Extract datetime.
	doc.Find("time[datetime]").Each(func(_ int, sel *goquery.Selection) {
		if dt, exists := sel.Attr("datetime"); exists && post.datetime == "" {
			post.datetime = dt
		}
	})

	return post, nil
}

// ukrainskaImageRef identifies one schedule image found in a post, kept in
// persisted state so it doesn't need to be re-downloaded or re-classified
// on later runs.
type ukrainskaImageRef struct {
	Checksum string `json:"checksum"`
	URL      string `json:"url"`
}

// ukrainskaPostRecord is what's remembered about a single scanned post. An
// empty ScheduleImages means the post was checked and had no schedule
// images — that's still worth remembering, so the post is never re-fetched.
type ukrainskaPostRecord struct {
	ScheduleImages []ukrainskaImageRef `json:"schedule_images,omitempty"`
}

// ukrainskaScanState is persisted across runs (see loadScanState /
// saveScanState) so each run only needs to fetch posts newer than the last
// scan, and previously found schedule images aren't lost just because
// they've scrolled past a fixed trailing window. Entries are only dropped
// once they're far behind the current scan frontier (see pruneOldPosts),
// not the moment their dates run out — a schedule with no remaining future
// dates is still valid output (the ingestion pipeline's own regression
// guard handles that), so evicting it early would leave a run with nothing
// to report at all once nothing new has been posted since.
type ukrainskaScanState struct {
	HighestScannedPost int                            `json:"highest_scanned_post"`
	Posts              map[string]ukrainskaPostRecord `json:"posts"`
}

func (s *UkrainskaScraper) loadScanState() ukrainskaScanState {
	var state ukrainskaScanState
	if s.store.GetJSON(ukrainskaScanStateKey, &state) && state.Posts != nil {
		return state
	}
	return ukrainskaScanState{Posts: make(map[string]ukrainskaPostRecord)}
}

func (s *UkrainskaScraper) saveScanState(state ukrainskaScanState) error {
	return s.store.SetJSON(ukrainskaScanStateKey, state)
}

// pruneOldPosts drops post records far behind the current scan frontier,
// purely to keep persisted state from growing forever — not because their
// content might be stale (which is harmless to keep around; see
// ukrainskaScanState). By the time a post falls this far behind, any
// schedule it held is certain to have long since been superseded in
// practice. Returns the number of records dropped.
func pruneOldPosts(state *ukrainskaScanState) int {
	cutoff := state.HighestScannedPost - ukrainskaMaxCatchUpPosts
	if cutoff <= 0 {
		return 0
	}
	dropped := 0
	for postKey := range state.Posts {
		postNum, err := strconv.Atoi(postKey)
		if err != nil || postNum < cutoff {
			delete(state.Posts, postKey)
			dropped++
		}
	}
	return dropped
}

// scheduleImage is a schedule image to OCR, tagged with enough to attribute
// extracted entries to a source URL and to update persisted scan state.
// data is only populated for images just downloaded this run — images
// carried forward from previous runs rely on the OCR cache instead (see
// ocrImage), falling back to a fresh download via url only on a cache miss.
type scheduleImage struct {
	checksum  string
	url       string
	data      []byte
	sourceURL string
	postKey   string
}

// findScheduleImages returns every currently-relevant schedule image: newly
// discovered ones from posts not yet in scan state, plus previously found
// ones carried forward from state. Only posts newer than
// state.HighestScannedPost (or, on a cold start, the trailing
// ukrainskaScanCount posts) are actually fetched — everything else is
// served from persisted state, so a steady-state run only does a handful of
// requests instead of re-scanning the whole window every time.
func (s *UkrainskaScraper) findScheduleImages(ctx context.Context, latestPost int, state *ukrainskaScanState) ([]scheduleImage, error) {
	newStart := state.HighestScannedPost + 1
	if state.HighestScannedPost == 0 {
		newStart = latestPost - ukrainskaScanCount + 1
	}
	if newStart < latestPost-ukrainskaMaxCatchUpPosts+1 {
		newStart = latestPost - ukrainskaMaxCatchUpPosts + 1
	}
	if newStart < 1 {
		newStart = 1
	}

	log.Printf("Ukrainska: scanning new posts %d to %d", newStart, latestPost)

	var found []scheduleImage
	newPostKeys := make(map[string]bool)
	postsChecked := 0
	postsWithImages := 0
	postsWithSchedules := 0
	// The high-water mark only advances past a post once we've recorded a
	// definitive result for it (found, or confirmed deleted). A transient
	// fetch failure stops it from advancing further, so that post — and
	// everything after it this run — gets retried from scratch next time,
	// instead of silently never being looked at again.
	highWaterMark := newStart - 1
	for postNum := newStart; postNum <= latestPost; postNum++ {
		post, err := s.fetchPost(ctx, postNum)
		if err != nil {
			if !errors.Is(err, errPostNotFound) {
				break
			}
			// Confirmed deleted/never existed — remember so it's never
			// retried, but there's nothing to scan.
			postKey := strconv.Itoa(postNum)
			newPostKeys[postKey] = true
			state.Posts[postKey] = ukrainskaPostRecord{}
			highWaterMark = postNum
			continue
		}
		postsChecked++

		postKey := strconv.Itoa(postNum)
		newPostKeys[postKey] = true
		highWaterMark = postNum
		var record ukrainskaPostRecord

		if len(post.imageURLs) > 0 {
			postsWithImages++
			sourceURL := fmt.Sprintf("%s/%d", ukrainskaChannelURL, postNum)
			for _, imgURL := range post.imageURLs {
				data, err := fetchURL(ctx, imgURL)
				if err != nil {
					log.Printf("Ukrainska: failed to download image from post %d: %v", postNum, err)
					continue
				}

				isSchedule, err := s.isScheduleImage(ctx, data)
				if err != nil {
					log.Printf("Ukrainska: classification failed for post %d image: %v", postNum, err)
					continue
				}
				if isSchedule {
					checksum := computeChecksum(data)
					record.ScheduleImages = append(record.ScheduleImages, ukrainskaImageRef{Checksum: checksum, URL: imgURL})
					found = append(found, scheduleImage{
						checksum:  checksum,
						url:       imgURL,
						data:      data,
						sourceURL: sourceURL,
						postKey:   postKey,
					})
				}
			}
			if len(record.ScheduleImages) > 0 {
				postsWithSchedules++
				log.Printf("Ukrainska: post %d (%s) contains schedule image(s)", postNum, post.datetime)
			}
		}
		state.Posts[postKey] = record
	}
	state.HighestScannedPost = highWaterMark

	log.Printf("Ukrainska: checked %d new post(s) (%d to %d), %d had images, %d had schedule images",
		postsChecked, newStart, latestPost, postsWithImages, postsWithSchedules)

	// Carry forward previously found schedule images from posts we didn't
	// just re-scan.
	carriedOver := 0
	for postKey, rec := range state.Posts {
		if newPostKeys[postKey] {
			continue
		}
		for _, ref := range rec.ScheduleImages {
			postNum, err := strconv.Atoi(postKey)
			if err != nil {
				continue
			}
			found = append(found, scheduleImage{
				checksum:  ref.Checksum,
				url:       ref.URL,
				sourceURL: fmt.Sprintf("%s/%d", ukrainskaChannelURL, postNum),
				postKey:   postKey,
			})
			carriedOver++
		}
	}

	s.note("scanned %d new post(s): %d schedule post(s) found; %d schedule image(s) carried over from earlier runs",
		postsChecked, postsWithSchedules, carriedOver)

	if len(found) == 0 {
		return nil, fmt.Errorf("no schedule images found (checked posts %d to %d, plus previously known)", newStart, latestPost)
	}

	// Carried-over images come from ranging over state.Posts, a map, whose
	// iteration order Go deliberately randomizes — left as-is, that would
	// reorder the output on every run even when nothing actually changed,
	// which defeats change-detection elsewhere (e.g. alert deduplication in
	// the ingestion job, which hashes the fetched services). Sort into a
	// stable order so output only changes when the underlying data does.
	sort.Slice(found, func(i, j int) bool {
		a, errA := strconv.Atoi(found[i].postKey)
		b, errB := strconv.Atoi(found[j].postKey)
		if errA != nil || errB != nil || a != b {
			return a < b
		}
		return found[i].checksum < found[j].checksum
	})

	return found, nil
}

// isScheduleImage checks whether an image is a church service schedule, with
// caching by image checksum to avoid redundant AI calls across ingestion runs.
func (s *UkrainskaScraper) isScheduleImage(ctx context.Context, imageData []byte) (bool, error) {
	checksum := computeChecksum(imageData)
	cacheKey := "ukrainska-classify/v1/" + checksum

	var cached bool
	if s.store.GetJSON(cacheKey, &cached) {
		log.Printf("Ukrainska: classification cache hit (checksum %s): %v", checksum[:12], cached)
		return cached, nil
	}

	result, err := s.vision.IsScheduleImage(ctx, imageData)
	if err != nil {
		return false, err
	}

	log.Printf("Ukrainska: classified image (checksum %s): schedule=%v", checksum[:12], result)

	// Cache the result.
	if data, merr := json.Marshal(result); merr == nil {
		if werr := s.store.SetRaw(cacheKey+".json", data); werr != nil {
			log.Printf("Ukrainska: failed to cache classification: %v", werr)
		}
	}

	return result, nil
}

// refetchImageFromPost re-fetches postKey's Telegram post and downloads
// whichever of its images currently matches checksum. Used when a
// previously-saved image URL has stopped working — Telegram's per-file URLs
// are signed and eventually expire, even though the image itself is still
// live on the post — so a fresh URL is needed rather than the one saved in
// scan state.
func (s *UkrainskaScraper) refetchImageFromPost(ctx context.Context, postKey, checksum string) ([]byte, error) {
	postNum, err := strconv.Atoi(postKey)
	if err != nil {
		return nil, fmt.Errorf("invalid post key %q: %w", postKey, err)
	}
	post, err := s.fetchPost(ctx, postNum)
	if err != nil {
		return nil, fmt.Errorf("re-fetching post %d: %w", postNum, err)
	}
	for _, url := range post.imageURLs {
		data, err := fetchURL(ctx, url)
		if err != nil {
			log.Printf("Ukrainska: failed to download refreshed image candidate from post %d: %v", postNum, err)
			continue
		}
		if computeChecksum(data) == checksum {
			return data, nil
		}
	}
	return nil, fmt.Errorf("no image in post %d matches checksum %s", postNum, checksum[:12])
}

// ocrImage extracts schedule entries from an image using the Vision API,
// with caching by image checksum. For images carried forward from earlier
// runs (img.data is nil), this normally hits the OCR cache directly without
// any download; on a cache miss it falls back to re-fetching img.url, and
// if that URL has since expired, to refetchImageFromPost.
func (s *UkrainskaScraper) ocrImage(ctx context.Context, img scheduleImage, sourceRef string) ([]vision.ScheduleEntry, error) {
	cacheKey := "ukrainska-ocr/v1/" + img.checksum

	// Check OCR cache.
	var raw vision.RawScheduleResult
	if s.store.GetJSON(cacheKey, &raw) {
		log.Printf("Ukrainska: OCR cache hit for %s (checksum %s)", sourceRef, img.checksum[:12])
	} else {
		log.Printf("Ukrainska: OCR cache miss for %s (checksum %s), calling API", sourceRef, img.checksum[:12])

		imageData := img.data
		if len(imageData) == 0 {
			var err error
			imageData, err = fetchURL(ctx, img.url)
			if err != nil {
				// Telegram's per-file URLs are signed and eventually expire,
				// so a URL saved in scan state days ago can start 404ing even
				// though the image itself is still live on the post — refetch
				// the post for a current URL instead of giving up.
				refreshed, rerr := s.refetchImageFromPost(ctx, img.postKey, img.checksum)
				if rerr != nil {
					return nil, fmt.Errorf("re-fetching %s for OCR: %w (refresh also failed: %v)", sourceRef, err, rerr)
				}
				log.Printf("Ukrainska: stale URL for %s, refreshed from post %s", sourceRef, img.postKey)
				imageData = refreshed
			}
		}

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

// deduplicate collapses services onto the same (date, time) slot. Different
// posts often describe the same service with different levels of detail
// (e.g. a terse weekly reminder vs. a fuller monthly schedule), so slots are
// merged by date+time rather than by exact name text, keeping the more
// detailed of the two descriptions.
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
		// SourceURL so the winner doesn't depend on input order (which two
		// posts describing the same slot get iterated in can otherwise
		// vary between runs — see findScheduleImages).
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
