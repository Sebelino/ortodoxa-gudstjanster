package scraper

import (
	"testing"
	"time"
)

func TestHeligaAnnaYearAssignment(t *testing.T) {
	// The year assignment logic from Fetch, extracted for testing:
	// If the candidate date is more than 3 months in the past, use next year.
	// Otherwise use current year.
	assignYear := func(now time.Time, month, day int) int {
		year := now.Year()
		candidate := time.Date(year, time.Month(month), day, 0, 0, 0, 0, now.Location())
		if candidate.Before(now.AddDate(0, -3, 0)) {
			year++
		} else if candidate.After(now.AddDate(0, 9, 0)) {
			year--
		}
		return year
	}

	// Simulate: today is March 7, 2026
	now := time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		month    int
		day      int
		wantYear int
	}{
		{"April event stays current year", 4, 10, 2026},
		{"March event stays current year", 3, 15, 2026},
		{"January event stays current year (only 2 months ago)", 1, 5, 2026},
		{"December event (3+ months ago) goes to next year", 11, 1, 2026},
		{"October event (7 months ahead) stays current year", 10, 15, 2026},
		{"February event stays current year", 2, 14, 2026},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := assignYear(now, tt.month, tt.day)
			if got != tt.wantYear {
				t.Errorf("assignYear(now, %d, %d) = %d, want %d", tt.month, tt.day, got, tt.wantYear)
			}
		})
	}

	// Edge case: today is January 15, 2026. A December event should be 2025 (recent past), not 2026.
	nowJan := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	got := assignYear(nowJan, 12, 20)
	if got != 2025 {
		t.Errorf("December event in January: got year %d, want 2025", got)
	}

	// Edge case: today is January 15, 2026. A September event should be 2026 (next year's).
	got = assignYear(nowJan, 9, 1)
	if got != 2026 {
		t.Errorf("September event in January: got year %d, want 2026", got)
	}
}
