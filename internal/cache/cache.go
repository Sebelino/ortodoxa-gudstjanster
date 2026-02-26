package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"ortodoxa-gudstjanster/internal/model"
)

// Entry represents a cached set of services with metadata.
type Entry struct {
	Services  []model.ChurchService `json:"services"`
	FetchedAt time.Time             `json:"fetched_at"`
}

// Cache provides disk-based caching for scraped services.
type Cache struct {
	dir string
	ttl time.Duration
	mu  sync.RWMutex
}

// New creates a new disk-based cache.
func New(cacheDir string, ttl time.Duration) (*Cache, error) {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}
	return &Cache{
		dir: cacheDir,
		ttl: ttl,
	}, nil
}

// Get retrieves cached services for a scraper if they exist and aren't expired.
func (c *Cache) Get(scraperName string) ([]model.ChurchService, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	path := c.filePath(scraperName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false
	}

	if time.Since(entry.FetchedAt) > c.ttl {
		return nil, false
	}

	return entry.Services, true
}

// Set stores services in the cache.
func (c *Cache) Set(scraperName string, services []model.ChurchService) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := Entry{
		Services:  services,
		FetchedAt: time.Now(),
	}

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(c.filePath(scraperName), data, 0644)
}

// Invalidate removes a specific scraper's cache.
func (c *Cache) Invalidate(scraperName string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	path := c.filePath(scraperName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// InvalidateAll removes all cached entries.
func (c *Cache) InvalidateAll() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".json" {
			os.Remove(filepath.Join(c.dir, entry.Name()))
		}
	}
	return nil
}

func (c *Cache) filePath(scraperName string) string {
	// Sanitize name to be filesystem-safe
	safeName := ""
	for _, r := range scraperName {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			safeName += string(r)
		} else {
			safeName += "_"
		}
	}
	return filepath.Join(c.dir, safeName+".json")
}
