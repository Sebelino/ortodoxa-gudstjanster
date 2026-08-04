package scraper

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
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

func TestLargestSrcsetURL(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{
			"picks widest variant",
			`<img src="https://gomos.se/img-725x1024.jpg" srcset="https://gomos.se/img-300x424.jpg 300w, https://gomos.se/img-725x1024.jpg 725w, https://gomos.se/img-1191x1682.jpg 1191w">`,
			"https://gomos.se/img-1191x1682.jpg",
		},
		{
			"single candidate",
			`<img src="https://gomos.se/img.jpg" srcset="https://gomos.se/img.jpg 500w">`,
			"https://gomos.se/img.jpg",
		},
		{
			"no srcset attribute",
			`<img src="https://gomos.se/img.jpg">`,
			"",
		},
		{
			"malformed srcset",
			`<img src="https://gomos.se/img.jpg" srcset="not a valid srcset">`,
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			if err != nil {
				t.Fatalf("parsing HTML: %v", err)
			}
			sel := doc.Find("img")
			got := largestSrcsetURL(sel)
			if got != tt.want {
				t.Errorf("largestSrcsetURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
