package scraper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/store"
	"ortodoxa-gudstjanster/internal/vision"
)

const (
	uploadsSourceName = "Uppladdade bilder"
)

// UploadsScraper processes uploaded images from a GCS bucket, using AI to extract
// event information from each image. The parish is determined by the top-level
// folder name, which must match a known parish slug.
type UploadsScraper struct {
	store      store.Store
	vision     *vision.Client
	reader     *store.BucketReader
	bucket     string
	parishInfo map[string]UploadParishInfo // slug → parish info
}

// UploadParishInfo holds the parish metadata needed by the uploads scraper.
type UploadParishInfo struct {
	Name       string
	Location   string
	Language   string
	SourceURL  string // if set, used as SourceURL instead of the GCS public URL
	SourceName string // if set, used as Source instead of "Uppladdade bilder"
}

// NewUploadsScraper creates a new scraper that processes uploaded images.
// The parishInfo map keys are parish slugs (matching bucket folder names)
// and values contain the parish name, location, and language.
func NewUploadsScraper(s store.Store, v *vision.Client, reader *store.BucketReader, bucket string, parishInfo map[string]UploadParishInfo) *UploadsScraper {
	return &UploadsScraper{
		store:      s,
		vision:     v,
		reader:     reader,
		bucket:     bucket,
		parishInfo: parishInfo,
	}
}

func (s *UploadsScraper) Name() string {
	return uploadsSourceName
}

func (s *UploadsScraper) Fetch(ctx context.Context) ([]model.ChurchService, error) {
	if s.reader == nil {
		log.Printf("Uploads scraper: no bucket reader configured, returning empty")
		return nil, nil
	}

	names, err := s.reader.ListObjects(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("listing upload objects: %w", err)
	}

	var allServices []model.ChurchService
	imageCount := 0

	for _, name := range names {
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".jpg") && !strings.HasSuffix(lower, ".jpeg") && !strings.HasSuffix(lower, ".png") {
			continue
		}

		// Extract parish slug from first path component (e.g. "helige-giorgis/maj-2026.jpg" → "helige-giorgis")
		slug, _, ok := strings.Cut(name, "/")
		if !ok {
			log.Printf("Uploads: skipping %s (not in a parish folder)", name)
			continue
		}

		parish, known := s.parishInfo[slug]
		if !known {
			log.Printf("Uploads: skipping %s (unknown parish slug %q)", name, slug)
			continue
		}

		imageData, err := s.reader.ReadObject(ctx, name)
		if err != nil {
			log.Printf("Uploads: failed to read %s: %v", name, err)
			continue
		}

		services, err := s.processImage(ctx, imageData, name, &parish)
		if err != nil {
			log.Printf("Uploads: failed to process %s: %v", name, err)
			continue
		}

		imageCount++
		allServices = append(allServices, services...)
	}

	log.Printf("Uploads: extracted %d services from %d images", len(allServices), imageCount)
	return allServices, nil
}

// processImage extracts events from a single image, with caching by checksum.
// The parish info from the folder slug overrides whatever the AI extracts.
func (s *UploadsScraper) processImage(ctx context.Context, imageData []byte, objectName string, parish *UploadParishInfo) ([]model.ChurchService, error) {
	checksum := computeChecksum(imageData)
	cacheKey := "uploads-ocr/v1/" + checksum

	var cached vision.ImageEventResult
	if s.store.GetJSON(cacheKey, &cached) {
		log.Printf("Uploads: cache hit for %s (checksum %s)", objectName, checksum[:12])
		return s.convertToServices(&cached, objectName, parish), nil
	}

	log.Printf("Uploads: cache miss for %s (checksum %s), calling API", objectName, checksum[:12])

	result, rawResponse, err := s.vision.ExtractEventsFromImage(ctx, imageData)
	if err != nil {
		return nil, fmt.Errorf("extracting events from %s: %w", objectName, err)
	}

	// Persist raw API response for diagnostics
	if werr := s.store.SetRaw(cacheKey+".response.txt", []byte(rawResponse)); werr != nil {
		log.Printf("Uploads: failed to persist response: %v", werr)
	}

	// Cache the result
	if data, merr := json.Marshal(result); merr == nil {
		if werr := s.store.SetRaw(cacheKey+".json", data); werr != nil {
			log.Printf("Uploads: failed to cache result: %v", werr)
		}
	}

	log.Printf("Uploads: extracted %d events from %s (parish: %s)", len(result.Events), objectName, parish.Name)
	return s.convertToServices(result, objectName, parish), nil
}

func (s *UploadsScraper) convertToServices(result *vision.ImageEventResult, objectName string, parish *UploadParishInfo) []model.ChurchService {
	sourceURL := parish.SourceURL
	if sourceURL == "" {
		sourceURL = fmt.Sprintf("https://storage.googleapis.com/%s/%s", s.bucket, objectName)
	}

	// Use parish info from slug; fall back to AI-extracted values
	parishName := parish.Name
	location := parish.Location
	if location == "" {
		location = result.Location
	}
	language := parish.Language
	if language == "" {
		language = result.Language
	}
	sourceName := parish.SourceName
	if sourceName == "" {
		sourceName = uploadsSourceName
	}

	var services []model.ChurchService
	for _, event := range result.Events {
		svc := model.ChurchService{
			Parish:      parishName,
			Source:      sourceName,
			SourceURL:   sourceURL,
			Date:        event.Date,
			DayOfWeek:   event.DayOfWeek,
			ServiceName: event.ServiceName,
		}

		if event.Time != "" {
			t := event.Time
			svc.Time = &t
		}

		if location != "" {
			loc := location
			svc.Location = &loc
		}

		if event.Occasion != "" {
			occ := event.Occasion
			svc.Occasion = &occ
		}

		if event.Notes != "" {
			notes := event.Notes
			svc.Notes = &notes
		}

		if language != "" {
			lang := language
			svc.Language = &lang
		}

		services = append(services, svc)
	}

	return services
}

func computeChecksum(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
