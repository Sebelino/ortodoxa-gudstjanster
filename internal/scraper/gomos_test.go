package scraper

import (
	"testing"
)

func TestImageExtension(t *testing.T) {
	s := &GomosScraper{}

	tests := []struct {
		url  string
		want string
	}{
		{"https://example.com/image.jpg", ".jpg"},
		{"https://example.com/image.png", ".png"},
		{"https://example.com/image.jpeg", ".jpeg"},
		{"https://example.com/image.JPG", ".jpg"},
		{"https://example.com/image.PNG", ".png"},
		{"https://example.com/image", ".jpg"},
		// Bug: Contains matches .png in the middle of the URL
		{"https://example.com/image.png.jpg", ".jpg"},
		{"https://example.com/image.jpeg.jpg", ".jpg"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := s.imageExtension(tt.url)
			if got != tt.want {
				t.Errorf("imageExtension(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}
