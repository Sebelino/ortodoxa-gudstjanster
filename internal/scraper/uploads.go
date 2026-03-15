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
// parish information and events from each image.
type UploadsScraper struct {
	store  store.Store
	vision *vision.Client
	reader *store.BucketReader
	bucket string
}

// NewUploadsScraper creates a new scraper that processes uploaded images.
func NewUploadsScraper(s store.Store, v *vision.Client, reader *store.BucketReader, bucket string) *UploadsScraper {
	return &UploadsScraper{
		store:  s,
		vision: v,
		reader: reader,
		bucket: bucket,
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

	for _, name := range names {
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".jpg") && !strings.HasSuffix(lower, ".jpeg") && !strings.HasSuffix(lower, ".png") {
			continue
		}

		// Skip gomos/ prefix — those images are handled by the Gomos scraper
		if strings.HasPrefix(name, "gomos/") {
			continue
		}

		imageData, err := s.reader.ReadObject(ctx, name)
		if err != nil {
			log.Printf("Uploads: failed to read %s: %v", name, err)
			continue
		}

		services, err := s.processImage(ctx, imageData, name)
		if err != nil {
			log.Printf("Uploads: failed to process %s: %v", name, err)
			continue
		}

		allServices = append(allServices, services...)
	}

	log.Printf("Uploads: extracted %d services from %d images", len(allServices), len(names))
	return allServices, nil
}

// processImage extracts events from a single image, with caching by checksum.
func (s *UploadsScraper) processImage(ctx context.Context, imageData []byte, objectName string) ([]model.ChurchService, error) {
	checksum := computeChecksum(imageData)
	cacheKey := "uploads-ocr/v1/" + checksum

	var cached vision.ImageEventResult
	if s.store.GetJSON(cacheKey, &cached) {
		log.Printf("Uploads: cache hit for %s (checksum %s)", objectName, checksum[:12])
		return s.convertToServices(&cached, objectName), nil
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

	log.Printf("Uploads: extracted %d events from %s (parish: %s)", len(result.Events), objectName, result.Parish)
	return s.convertToServices(result, objectName), nil
}

func (s *UploadsScraper) convertToServices(result *vision.ImageEventResult, objectName string) []model.ChurchService {
	publicURL := fmt.Sprintf("https://storage.googleapis.com/%s/%s", s.bucket, objectName)

	var services []model.ChurchService
	for _, event := range result.Events {
		svc := model.ChurchService{
			Parish:      result.Parish,
			Source:      uploadsSourceName,
			SourceURL:   publicURL,
			Date:        event.Date,
			DayOfWeek:   event.DayOfWeek,
			ServiceName: event.ServiceName,
		}

		if event.Time != "" {
			t := event.Time
			svc.Time = &t
		}

		if result.Location != "" {
			loc := result.Location
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

		if result.Language != "" {
			lang := result.Language
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
