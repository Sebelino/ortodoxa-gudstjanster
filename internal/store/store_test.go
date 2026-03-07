package store

import (
	"os"
	"path/filepath"
	"testing"
)

func tempStoreDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(os.TempDir(), "ortodoxa-store-test-"+t.Name())
	os.RemoveAll(dir)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestLocalStoreSetAndGet(t *testing.T) {
	s, err := NewLocal(tempStoreDir(t))
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}

	if err := s.Set("mykey", []byte("hello")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	data, ok := s.Get("mykey")
	if !ok {
		t.Fatal("Get returned false for existing key")
	}
	if string(data) != "hello" {
		t.Errorf("Get = %q, want %q", string(data), "hello")
	}
}

func TestLocalStoreGetMiss(t *testing.T) {
	s, err := NewLocal(tempStoreDir(t))
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}

	_, ok := s.Get("nonexistent")
	if ok {
		t.Error("Get should return false for missing key")
	}
}

func TestLocalStoreSetJSON(t *testing.T) {
	s, err := NewLocal(tempStoreDir(t))
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}

	type payload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	input := payload{Name: "test", Count: 42}
	if err := s.SetJSON("data", input); err != nil {
		t.Fatalf("SetJSON: %v", err)
	}

	var output payload
	if !s.GetJSON("data", &output) {
		t.Fatal("GetJSON returned false")
	}
	if output.Name != "test" || output.Count != 42 {
		t.Errorf("GetJSON = %+v, want %+v", output, input)
	}
}

func TestLocalStoreSetWithExtension(t *testing.T) {
	dir := tempStoreDir(t)
	s, err := NewLocal(dir)
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}

	if err := s.SetWithExtension("image", ".png", []byte("fakepng")); err != nil {
		t.Fatalf("SetWithExtension: %v", err)
	}

	path := filepath.Join(dir, "image.png")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not found at %s: %v", path, err)
	}
	if string(data) != "fakepng" {
		t.Errorf("file content = %q, want %q", string(data), "fakepng")
	}
}

func TestLocalStoreSetRaw(t *testing.T) {
	dir := tempStoreDir(t)
	s, err := NewLocal(dir)
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}

	if err := s.SetRaw("sub/dir/file.txt", []byte("content")); err != nil {
		t.Fatalf("SetRaw: %v", err)
	}

	path := filepath.Join(dir, "sub/dir/file.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not found at %s: %v", path, err)
	}
	if string(data) != "content" {
		t.Errorf("file content = %q, want %q", string(data), "content")
	}
}

func TestLocalStoreSetCreatesParentDirs(t *testing.T) {
	dir := tempStoreDir(t)
	s, err := NewLocal(dir)
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}

	if err := s.Set("nested/deep/key", []byte("value")); err != nil {
		t.Fatalf("Set with nested key: %v", err)
	}

	data, ok := s.Get("nested/deep/key")
	if !ok {
		t.Fatal("Get returned false for nested key")
	}
	if string(data) != "value" {
		t.Errorf("Get = %q, want %q", string(data), "value")
	}
}

func TestLocalStoreOverwrite(t *testing.T) {
	s, err := NewLocal(tempStoreDir(t))
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}

	s.Set("key", []byte("first"))
	s.Set("key", []byte("second"))

	data, ok := s.Get("key")
	if !ok {
		t.Fatal("Get returned false")
	}
	if string(data) != "second" {
		t.Errorf("Get = %q, want %q", string(data), "second")
	}
}
