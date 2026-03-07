package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ortodoxa-gudstjanster/internal/model"
)

func tempCacheDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(os.TempDir(), "ortodoxa-cache-test-"+t.Name())
	os.RemoveAll(dir)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestCacheSetAndGet(t *testing.T) {
	c, err := New(tempCacheDir(t), time.Hour)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	services := []model.ChurchService{
		{Source: "Test", Date: "2026-03-08", ServiceName: "Liturgi"},
	}

	if err := c.Set("test-scraper", services); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, ok := c.Get("test-scraper")
	if !ok {
		t.Fatal("Get returned false for cached entry")
	}
	if len(got) != 1 || got[0].ServiceName != "Liturgi" {
		t.Errorf("Get returned unexpected data: %v", got)
	}
}

func TestCacheGetMiss(t *testing.T) {
	c, err := New(tempCacheDir(t), time.Hour)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, ok := c.Get("nonexistent")
	if ok {
		t.Error("Get should return false for missing entry")
	}
}

func TestCacheTTLExpiry(t *testing.T) {
	c, err := New(tempCacheDir(t), 10*time.Millisecond)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	services := []model.ChurchService{
		{Source: "Test", Date: "2026-03-08", ServiceName: "Liturgi"},
	}

	if err := c.Set("test-scraper", services); err != nil {
		t.Fatalf("Set: %v", err)
	}

	time.Sleep(15 * time.Millisecond)

	_, ok := c.Get("test-scraper")
	if ok {
		t.Error("Get should return false after TTL expiry")
	}
}

func TestCacheInvalidate(t *testing.T) {
	c, err := New(tempCacheDir(t), time.Hour)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	services := []model.ChurchService{
		{Source: "Test", Date: "2026-03-08", ServiceName: "Liturgi"},
	}

	c.Set("scraper-a", services)
	c.Set("scraper-b", services)

	if err := c.Invalidate("scraper-a"); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	if _, ok := c.Get("scraper-a"); ok {
		t.Error("scraper-a should be invalidated")
	}
	if _, ok := c.Get("scraper-b"); !ok {
		t.Error("scraper-b should still be cached")
	}
}

func TestCacheInvalidateAll(t *testing.T) {
	c, err := New(tempCacheDir(t), time.Hour)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	services := []model.ChurchService{
		{Source: "Test", Date: "2026-03-08", ServiceName: "Liturgi"},
	}

	c.Set("scraper-a", services)
	c.Set("scraper-b", services)

	if err := c.InvalidateAll(); err != nil {
		t.Fatalf("InvalidateAll: %v", err)
	}

	if _, ok := c.Get("scraper-a"); ok {
		t.Error("scraper-a should be invalidated")
	}
	if _, ok := c.Get("scraper-b"); ok {
		t.Error("scraper-b should be invalidated")
	}
}

func TestCacheInvalidateNonexistent(t *testing.T) {
	c, err := New(tempCacheDir(t), time.Hour)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := c.Invalidate("nonexistent"); err != nil {
		t.Errorf("Invalidate of nonexistent entry should not error: %v", err)
	}
}

func TestCacheFilePathSanitization(t *testing.T) {
	c, err := New(tempCacheDir(t), time.Hour)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	services := []model.ChurchService{
		{Source: "Test", Date: "2026-03-08", ServiceName: "Liturgi"},
	}

	// Name with special characters should work
	if err := c.Set("St. Georgios Cathedral", services); err != nil {
		t.Fatalf("Set with special chars: %v", err)
	}

	got, ok := c.Get("St. Georgios Cathedral")
	if !ok {
		t.Error("Get should find entry with sanitized name")
	}
	if len(got) != 1 {
		t.Errorf("expected 1 service, got %d", len(got))
	}
}
