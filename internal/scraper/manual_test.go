package scraper

import (
	"testing"
	"time"

	"ortodoxa-gudstjanster/internal/model"
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

func TestBuildManualService(t *testing.T) {
	event := RecurringEvent{
		Parish:       "Test Parish",
		Source:       "Test Source",
		ServiceName:  "Liturgi",
		Location:     "Stockholm",
		StartTimeStr: "10:00",
		EndTimeStr:   "11:30",
		Language:     "Svenska",
	}
	date := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)

	svc := buildManualService(event, date)

	if svc.Time == nil {
		t.Fatal("Time should be set when StartTimeStr is provided")
	}
	if *svc.Time != "10:00" {
		t.Errorf("Time = %q, want %q", *svc.Time, "10:00")
	}
	if svc.StartTime == nil {
		t.Fatal("StartTime should be set")
	}
	if svc.EndTime == nil {
		t.Fatal("EndTime should be set")
	}
	if svc.Parish != "Test Parish" {
		t.Errorf("Parish = %q, want %q", svc.Parish, "Test Parish")
	}
	if svc.DayOfWeek != "Söndag" {
		t.Errorf("DayOfWeek = %q, want %q", svc.DayOfWeek, "Söndag")
	}
}

func TestBuildManualServiceNoTime(t *testing.T) {
	event := RecurringEvent{
		Parish:      "Test",
		Source:      "Test",
		ServiceName: "Fastedag",
		Location:    "Stockholm",
		Language:    "Svenska",
	}
	date := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)

	svc := buildManualService(event, date)

	if svc.Time != nil {
		t.Errorf("Time should be nil when no StartTimeStr, got %q", *svc.Time)
	}
	if svc.StartTime != nil {
		t.Error("StartTime should be nil")
	}
}

func TestBuildManualServiceOptionalFields(t *testing.T) {
	event := RecurringEvent{
		Parish:         "Test",
		Source:         "Test",
		ServiceName:    "Liturgi",
		Location:       "Stockholm",
		StartTimeStr:   "09:00",
		Language:       "Svenska",
		ParishLanguage: "Svenska, finska",
		EventLanguage:  "Svenska",
		Notes:          "Bring candles",
		Title:          "Liturgi",
	}
	date := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)

	svc := buildManualService(event, date)

	if svc.ParishLanguage == nil || *svc.ParishLanguage != "Svenska, finska" {
		t.Errorf("ParishLanguage = %v, want %q", svc.ParishLanguage, "Svenska, finska")
	}
	if svc.EventLanguage == nil || *svc.EventLanguage != "Svenska" {
		t.Errorf("EventLanguage = %v, want %q", svc.EventLanguage, "Svenska")
	}
	if svc.Notes == nil || *svc.Notes != "Bring candles" {
		t.Errorf("Notes = %v, want %q", svc.Notes, "Bring candles")
	}
	if svc.Title != "Liturgi" {
		t.Errorf("Title = %q, want %q", svc.Title, "Liturgi")
	}
}

// Verify the function signature exists (will fail to compile if missing)
var _ = func(e RecurringEvent, d time.Time) model.ChurchService {
	return buildManualService(e, d)
}

