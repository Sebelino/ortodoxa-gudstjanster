package store

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"cloud.google.com/go/storage"
)

// GCSStore is a Cloud Storage-backed implementation of Store.
type GCSStore struct {
	client *storage.Client
	bucket string
	mu     sync.RWMutex
}

// NewGCS creates a new GCSStore with the specified bucket.
func NewGCS(ctx context.Context, bucket string) (*GCSStore, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return &GCSStore{
		client: client,
		bucket: bucket,
	}, nil
}

// Get retrieves a value by key. Returns the value and true if found,
// or nil and false if not found.
func (s *GCSStore) Get(key string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	obj := s.client.Bucket(s.bucket).Object(s.keyPath(key))
	reader, err := obj.NewReader(ctx)
	if err != nil {
		return nil, false
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, false
	}
	return data, true
}

// Set stores a value with the given key.
func (s *GCSStore) Set(key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	obj := s.client.Bucket(s.bucket).Object(s.keyPath(key))
	writer := obj.NewWriter(ctx)
	writer.ContentType = "application/json"

	if _, err := writer.Write(value); err != nil {
		writer.Close()
		return err
	}
	return writer.Close()
}

// GetJSON retrieves and unmarshals a JSON value.
func (s *GCSStore) GetJSON(key string, v interface{}) bool {
	data, ok := s.Get(key)
	if !ok {
		return false
	}
	return json.Unmarshal(data, v) == nil
}

// SetJSON marshals and stores a value as JSON.
func (s *GCSStore) SetJSON(key string, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.Set(key, data)
}

// SetWithExtension stores raw bytes with a custom file extension.
func (s *GCSStore) SetWithExtension(key string, ext string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	obj := s.client.Bucket(s.bucket).Object(key + ext)
	writer := obj.NewWriter(ctx)

	// Set content type based on extension
	switch ext {
	case ".jpg", ".jpeg":
		writer.ContentType = "image/jpeg"
	case ".png":
		writer.ContentType = "image/png"
	default:
		writer.ContentType = "application/octet-stream"
	}

	if _, err := writer.Write(value); err != nil {
		writer.Close()
		return err
	}
	return writer.Close()
}

// Close closes the GCS client.
func (s *GCSStore) Close() error {
	return s.client.Close()
}

func (s *GCSStore) keyPath(key string) string {
	return key + ".json"
}
