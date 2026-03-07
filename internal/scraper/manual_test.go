package scraper

import (
	"testing"
	"time"
)

func TestParseHHMM(t *testing.T) {
	date := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		input   string
		wantH   int
		wantM   int
		wantErr bool
	}{
		{"10:00", 10, 0, false},
		{"9:30", 9, 30, false},
		{"18:45", 18, 45, false},
		{"invalid", 0, 0, true},
		{"", 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseHHMM(date, tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Hour() != tt.wantH || got.Minute() != tt.wantM {
				t.Errorf("parseHHMM(%q) = %02d:%02d, want %02d:%02d", tt.input, got.Hour(), got.Minute(), tt.wantH, tt.wantM)
			}
			if got.Year() != 2026 || got.Month() != 3 || got.Day() != 8 {
				t.Errorf("date portion wrong: %v", got)
			}
		})
	}
}

func TestSwedishDayOfWeek(t *testing.T) {
	tests := []struct {
		day  time.Weekday
		want string
	}{
		{time.Monday, "Måndag"},
		{time.Tuesday, "Tisdag"},
		{time.Wednesday, "Onsdag"},
		{time.Thursday, "Torsdag"},
		{time.Friday, "Fredag"},
		{time.Saturday, "Lördag"},
		{time.Sunday, "Söndag"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := swedishDayOfWeek(tt.day)
			if got != tt.want {
				t.Errorf("swedishDayOfWeek(%v) = %q, want %q", tt.day, got, tt.want)
			}
		})
	}
}
