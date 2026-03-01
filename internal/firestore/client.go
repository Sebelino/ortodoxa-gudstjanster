package firestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

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

// ReplaceServicesForSource atomically replaces all services for a source.
// It deletes all existing documents for the source, then writes the new ones.
func (c *Client) ReplaceServicesForSource(ctx context.Context, source string, services []model.ChurchService, batchID string) error {
	coll := c.client.Collection(c.collection)

	// First, delete all existing documents for this source
	if err := c.deleteServicesForSource(ctx, source); err != nil {
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
			batch.Set(doc, serviceToMap(svc, batchID))
		}

		if _, err := batch.Commit(ctx); err != nil {
			return fmt.Errorf("committing batch: %w", err)
		}
	}

	return nil
}

// deleteServicesForSource deletes all documents for a given source.
func (c *Client) deleteServicesForSource(ctx context.Context, source string) error {
	coll := c.client.Collection(c.collection)
	query := coll.Where("source", "==", source)

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
func serviceToMap(svc model.ChurchService, batchID string) map[string]interface{} {
	m := map[string]interface{}{
		"source":       svc.Source,
		"date":         svc.Date,
		"day_of_week":  svc.DayOfWeek,
		"service_name": svc.ServiceName,
		"batch_id":     batchID,
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
	return m
}

// mapToService converts a Firestore document map to a ChurchService.
func mapToService(m map[string]interface{}) (model.ChurchService, error) {
	svc := model.ChurchService{}

	if v, ok := m["source"].(string); ok {
		svc.Source = v
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

	return svc, nil
}
