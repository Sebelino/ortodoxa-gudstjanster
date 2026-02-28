package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Store is the interface for a persistent key-value store.
// Unlike cache, it has no TTL - data persists indefinitely.
type Store interface {
	Get(key string) ([]byte, bool)
	Set(key string, value []byte) error
	GetJSON(key string, v interface{}) bool
	SetJSON(key string, v interface{}) error
	SetWithExtension(key string, ext string, value []byte) error
}

// LocalStore is a file-based implementation of Store.
type LocalStore struct {
	dir string
	mu  sync.RWMutex
}

// NewLocal creates a new LocalStore with the specified directory.
func NewLocal(dir string) (*LocalStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	return &LocalStore{dir: dir}, nil
}

// Get retrieves a value by key. Returns the value and true if found,
// or nil and false if not found.
func (s *LocalStore) Get(key string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.keyPath(key)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}

// Set stores a value with the given key.
func (s *LocalStore) Set(key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.keyPath(key)
	return os.WriteFile(path, value, 0644)
}

// GetJSON retrieves and unmarshals a JSON value.
func (s *LocalStore) GetJSON(key string, v interface{}) bool {
	data, ok := s.Get(key)
	if !ok {
		return false
	}
	return json.Unmarshal(data, v) == nil
}

// SetJSON marshals and stores a value as JSON.
func (s *LocalStore) SetJSON(key string, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.Set(key, data)
}

// SetWithExtension stores raw bytes with a custom file extension.
func (s *LocalStore) SetWithExtension(key string, ext string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, key+ext)
	return os.WriteFile(path, value, 0644)
}

func (s *LocalStore) keyPath(key string) string {
	return filepath.Join(s.dir, key+".json")
}
