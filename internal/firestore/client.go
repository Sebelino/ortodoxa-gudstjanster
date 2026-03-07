package firestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"ortodoxa-gudstjanster/internal/model"
)

const batchSize = 250 // Stay well under Firestore's 500 operation limit

// Client wraps the Firestore client for church service operations.
type Client struct {
	client     *firestore.Client
	collection string
}

// New creates a new Firestore client.
func New(ctx context.Context, projectID, collection string) (*Client, error) {
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("creating firestore client: %w", err)
	}
	return &Client{
		client:     client,
		collection: collection,
	}, nil
}

// Close closes the Firestore client.
func (c *Client) Close() error {
	return c.client.Close()
}

// ReplaceServicesForScraper atomically replaces all services for a scraper.
// It deletes all existing documents for the scraper, then writes the new ones.
func (c *Client) ReplaceServicesForScraper(ctx context.Context, scraperName string, services []model.ChurchService, batchID string) error {
	coll := c.client.Collection(c.collection)

	// First, delete all existing documents for this scraper
	if err := c.deleteServicesForScraper(ctx, scraperName); err != nil {
		return fmt.Errorf("deleting existing services: %w", err)
	}

	// Then, write new documents in batches
	for i := 0; i < len(services); i += batchSize {
		end := i + batchSize
		if end > len(services) {
			end = len(services)
		}
		batch := c.client.Batch()

		for _, svc := range services[i:end] {
			docID := generateDocID(svc)
			doc := coll.Doc(docID)
			batch.Set(doc, serviceToMap(svc, scraperName, batchID))
		}

		if _, err := batch.Commit(ctx); err != nil {
			return fmt.Errorf("committing batch: %w", err)
		}
	}

	return nil
}

// deleteServicesForScraper deletes all documents for a given scraper.
// It first deletes by scraper_name (new docs), then cleans up legacy docs
// where source matches but scraper_name is absent.
func (c *Client) deleteServicesForScraper(ctx context.Context, scraperName string) error {
	coll := c.client.Collection(c.collection)

	// Delete docs with scraper_name == scraperName
	if err := c.deleteDocs(ctx, coll.Where("scraper_name", "==", scraperName)); err != nil {
		return err
	}

	// Legacy cleanup: delete docs where source == scraperName and scraper_name is absent
	iter := coll.Where("source", "==", scraperName).Documents(ctx)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return fmt.Errorf("iterating legacy documents: %w", err)
		}
		if _, hasScraper := doc.Data()["scraper_name"]; !hasScraper {
			if _, err := doc.Ref.Delete(ctx); err != nil {
				return fmt.Errorf("deleting legacy document: %w", err)
			}
		}
	}

	return nil
}

// deleteDocs deletes all documents matching a query in batches.
func (c *Client) deleteDocs(ctx context.Context, query firestore.Query) error {
	for {
		iter := query.Limit(batchSize).Documents(ctx)
		batch := c.client.Batch()
		numDeleted := 0

		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return fmt.Errorf("iterating documents: %w", err)
			}
			batch.Delete(doc.Ref)
			numDeleted++
		}

		if numDeleted == 0 {
			return nil
		}

		if _, err := batch.Commit(ctx); err != nil {
			return fmt.Errorf("committing delete batch: %w", err)
		}

		if numDeleted < batchSize {
			return nil
		}
	}
}

// GetAllServices retrieves all services from Firestore.
func (c *Client) GetAllServices(ctx context.Context) ([]model.ChurchService, error) {
	var services []model.ChurchService

	iter := c.client.Collection(c.collection).Documents(ctx)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("iterating documents: %w", err)
		}

		svc, err := mapToService(doc.Data())
		if err != nil {
			return nil, fmt.Errorf("parsing document %s: %w", doc.Ref.ID, err)
		}
		services = append(services, svc)
	}

	return services, nil
}

// CountServicesForScraper returns the number of stored services for a given scraper.
func (c *Client) CountServicesForScraper(ctx context.Context, scraperName string) (int, error) {
	query := c.client.Collection(c.collection).Where("scraper_name", "==", scraperName)
	count := 0

	iter := query.Documents(ctx)
	for {
		_, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("counting documents for scraper %s: %w", scraperName, err)
		}
		count++
	}

	return count, nil
}

// CountFutureServicesForScraper returns the number of stored services for a
// given scraper with date >= today (YYYY-MM-DD).
func (c *Client) CountFutureServicesForScraper(ctx context.Context, scraperName string) (int, error) {
	today := time.Now().Format("2006-01-02")
	query := c.client.Collection(c.collection).
		Where("scraper_name", "==", scraperName).
		Where("date", ">=", today)
	count := 0

	iter := query.Documents(ctx)
	for {
		_, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("counting future documents for scraper %s: %w", scraperName, err)
		}
		count++
	}

	return count, nil
}

// DeletePastServices deletes all services with date before today.
func (c *Client) DeletePastServices(ctx context.Context) (int, error) {
	today := time.Now().Format("2006-01-02")
	query := c.client.Collection(c.collection).Where("date", "<", today)
	total := 0

	for {
		iter := query.Limit(batchSize).Documents(ctx)
		batch := c.client.Batch()
		n := 0

		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return total, fmt.Errorf("iterating past documents: %w", err)
			}
			batch.Delete(doc.Ref)
			n++
		}

		if n == 0 {
			return total, nil
		}

		if _, err := batch.Commit(ctx); err != nil {
			return total, fmt.Errorf("committing delete batch: %w", err)
		}
		total += n

		if n < batchSize {
			return total, nil
		}
	}
}

// GetLatestBatchID returns the most recent batch_id from the collection.
func (c *Client) GetLatestBatchID(ctx context.Context) (string, error) {
	iter := c.client.Collection(c.collection).
		OrderBy("batch_id", firestore.Desc).
		Limit(1).
		Documents(ctx)

	doc, err := iter.Next()
	if err == iterator.Done {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("querying latest batch_id: %w", err)
	}

	batchID, _ := doc.Data()["batch_id"].(string)
	return batchID, nil
}

// generateDocID creates a unique document ID based on service fields.
func generateDocID(svc model.ChurchService) string {
	timeStr := ""
	if svc.Time != nil {
		timeStr = *svc.Time
	}
	data := fmt.Sprintf("%s|%s|%s|%s", svc.Source, svc.Date, svc.ServiceName, timeStr)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:16]) // Use first 16 bytes for shorter ID
}

// serviceToMap converts a ChurchService to a Firestore document map.
func serviceToMap(svc model.ChurchService, scraperName string, batchID string) map[string]interface{} {
	m := map[string]interface{}{
		"parish":       svc.Parish,
		"source":       svc.Source,
		"scraper_name": scraperName,
		"date":         svc.Date,
		"day_of_week":  svc.DayOfWeek,
		"service_name": svc.ServiceName,
		"batch_id":     batchID,
	}
	if svc.Title != "" {
		m["title"] = svc.Title
	}
	if svc.SourceURL != "" {
		m["source_url"] = svc.SourceURL
	}
	if svc.Location != nil {
		m["location"] = *svc.Location
	}
	if svc.Time != nil {
		m["time"] = *svc.Time
	}
	if svc.Occasion != nil {
		m["occasion"] = *svc.Occasion
	}
	if svc.Notes != nil {
		m["notes"] = *svc.Notes
	}
	if svc.Language != nil {
		m["language"] = *svc.Language
	}
	if svc.ParishLanguage != nil {
		m["parish_language"] = *svc.ParishLanguage
	}
	if svc.EventLanguage != nil {
		m["event_language"] = *svc.EventLanguage
	}
	if svc.StartTime != nil {
		m["start_time"] = svc.StartTime.Format(time.RFC3339)
	}
	if svc.EndTime != nil {
		m["end_time"] = svc.EndTime.Format(time.RFC3339)
	}
	return m
}

// mapToService converts a Firestore document map to a ChurchService.
func mapToService(m map[string]interface{}) (model.ChurchService, error) {
	svc := model.ChurchService{}

	if v, ok := m["parish"].(string); ok {
		svc.Parish = v
	}
	if v, ok := m["source"].(string); ok {
		svc.Source = v
	}
	// Fall back to source if parish is absent (backward compat with existing docs)
	if svc.Parish == "" {
		svc.Parish = svc.Source
	}
	if v, ok := m["source_url"].(string); ok {
		svc.SourceURL = v
	}
	if v, ok := m["date"].(string); ok {
		svc.Date = v
	}
	if v, ok := m["day_of_week"].(string); ok {
		svc.DayOfWeek = v
	}
	if v, ok := m["service_name"].(string); ok {
		svc.ServiceName = v
	}
	if v, ok := m["title"].(string); ok {
		svc.Title = v
	}
	if v, ok := m["location"].(string); ok {
		svc.Location = &v
	}
	if v, ok := m["time"].(string); ok {
		svc.Time = &v
	}
	if v, ok := m["occasion"].(string); ok {
		svc.Occasion = &v
	}
	if v, ok := m["notes"].(string); ok {
		svc.Notes = &v
	}
	if v, ok := m["language"].(string); ok {
		svc.Language = &v
	}
	if v, ok := m["parish_language"].(string); ok {
		svc.ParishLanguage = &v
	} else if svc.Language != nil {
		// Backward compat: use language as parish_language if absent
		svc.ParishLanguage = svc.Language
	}
	if v, ok := m["event_language"].(string); ok {
		svc.EventLanguage = &v
	}
	if v, ok := m["start_time"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			svc.StartTime = &t
		}
	}
	if v, ok := m["end_time"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			svc.EndTime = &t
		}
	}

	return svc, nil
}
