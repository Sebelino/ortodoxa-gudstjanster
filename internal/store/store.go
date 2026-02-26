package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Store is a persistent key-value store backed by JSON files.
// Unlike cache, it has no TTL - data persists indefinitely.
type Store struct {
	dir string
	mu  sync.RWMutex
}

// New creates a new Store with the specified directory.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Get retrieves a value by key. Returns the value and true if found,
// or nil and false if not found.
func (s *Store) Get(key string) ([]byte, bool) {
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
func (s *Store) Set(key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.keyPath(key)
	return os.WriteFile(path, value, 0644)
}

// GetJSON retrieves and unmarshals a JSON value.
func (s *Store) GetJSON(key string, v interface{}) bool {
	data, ok := s.Get(key)
	if !ok {
		return false
	}
	return json.Unmarshal(data, v) == nil
}

// SetJSON marshals and stores a value as JSON.
func (s *Store) SetJSON(key string, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.Set(key, data)
}

// SetWithExtension stores raw bytes with a custom file extension.
func (s *Store) SetWithExtension(key string, ext string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, key+ext)
	return os.WriteFile(path, value, 0644)
}

func (s *Store) keyPath(key string) string {
	return filepath.Join(s.dir, key+".json")
}
