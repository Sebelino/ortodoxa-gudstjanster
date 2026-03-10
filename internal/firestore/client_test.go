package firestore

import (
	"testing"
	"time"

	"ortodoxa-gudstjanster/internal/model"
)

func ptr(s string) *string { return &s }

func TestGenerateDocID(t *testing.T) {
	svc := model.ChurchService{
		Source:      "Test Parish",
		Date:        "2026-03-08",
		ServiceName: "Liturgi",
		Time:        ptr("10:00"),
	}

	id1 := generateDocID(svc)
	id2 := generateDocID(svc)

	if id1 != id2 {
		t.Error("same input should produce same doc ID")
	}
	if len(id1) != 32 {
		t.Errorf("doc ID length = %d, want 32 hex chars", len(id1))
	}

	// Different time → different ID
	svc2 := svc
	svc2.Time = ptr("11:00")
	if generateDocID(svc2) == id1 {
		t.Error("different time should produce different doc ID")
	}

	// Nil time → different from non-nil
	svc3 := svc
	svc3.Time = nil
	if generateDocID(svc3) == id1 {
		t.Error("nil time should produce different doc ID than non-nil")
	}
}

func TestServiceToMapAndBack(t *testing.T) {
	loc := "Stockholm"
	timeStr := "10:00"
	occasion := "Pascha"
	notes := "Bring candles"
	lang := "Svenska"
	pl := "Svenska, finska"
	el := "Svenska"
	startTime := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	endTime := time.Date(2026, 3, 8, 11, 0, 0, 0, time.UTC)

	original := model.ChurchService{
		Parish:         "Test Parish",
		Source:         "Test Source",
		SourceURL:      "https://example.com",
		Date:           "2026-03-08",
		DayOfWeek:      "Söndag",
		ServiceName:    "Helig Liturgi",
		Title:          "Liturgi",
		Location:       &loc,
		Time:           &timeStr,
		Occasion:       &occasion,
		Notes:          &notes,
		Language:       &lang,
		ParishLanguage: &pl,
		EventLanguage:  &el,
		StartTime:      &startTime,
		EndTime:        &endTime,
	}

	m := serviceToMap(original, "test-scraper", "batch-001")

	// Verify map has expected fields
	if m["parish"] != "Test Parish" {
		t.Errorf("parish = %v", m["parish"])
	}
	if m["scraper_name"] != "test-scraper" {
		t.Errorf("scraper_name = %v", m["scraper_name"])
	}
	if m["batch_id"] != "batch-001" {
		t.Errorf("batch_id = %v", m["batch_id"])
	}
	if m["title"] != "Liturgi" {
		t.Errorf("title = %v", m["title"])
	}

	// Convert start_time/end_time back to string format for mapToService
	// (mapToService expects string values from Firestore)
	roundtrip, err := mapToService(m)
	if err != nil {
		t.Fatalf("mapToService: %v", err)
	}

	if roundtrip.Parish != original.Parish {
		t.Errorf("Parish = %q, want %q", roundtrip.Parish, original.Parish)
	}
	if roundtrip.Source != original.Source {
		t.Errorf("Source = %q, want %q", roundtrip.Source, original.Source)
	}
	if roundtrip.Date != original.Date {
		t.Errorf("Date = %q, want %q", roundtrip.Date, original.Date)
	}
	if roundtrip.ServiceName != original.ServiceName {
		t.Errorf("ServiceName = %q, want %q", roundtrip.ServiceName, original.ServiceName)
	}
	if roundtrip.Title != original.Title {
		t.Errorf("Title = %q, want %q", roundtrip.Title, original.Title)
	}
	if roundtrip.Location == nil || *roundtrip.Location != loc {
		t.Errorf("Location = %v, want %q", roundtrip.Location, loc)
	}
	if roundtrip.Time == nil || *roundtrip.Time != timeStr {
		t.Errorf("Time = %v, want %q", roundtrip.Time, timeStr)
	}
	if roundtrip.ParishLanguage == nil || *roundtrip.ParishLanguage != pl {
		t.Errorf("ParishLanguage = %v, want %q", roundtrip.ParishLanguage, pl)
	}
	if roundtrip.EventLanguage == nil || *roundtrip.EventLanguage != el {
		t.Errorf("EventLanguage = %v, want %q", roundtrip.EventLanguage, el)
	}
}

func TestMapToServiceParishFallback(t *testing.T) {
	m := map[string]interface{}{
		"source":       "Legacy Source",
		"date":         "2026-03-08",
		"service_name": "Liturgi",
	}

	svc, err := mapToService(m)
	if err != nil {
		t.Fatalf("mapToService: %v", err)
	}

	if svc.Parish != "Legacy Source" {
		t.Errorf("Parish = %q, want %q (should fall back to Source)", svc.Parish, "Legacy Source")
	}
}

func TestMapToServiceEmptyParishStaysEmpty(t *testing.T) {
	m := map[string]interface{}{
		"parish":       "",
		"source":       "Some Source",
		"date":         "2026-03-08",
		"service_name": "Event",
	}

	svc, err := mapToService(m)
	if err != nil {
		t.Fatalf("mapToService: %v", err)
	}

	if svc.Parish != "" {
		t.Errorf("Parish = %q, want empty (should not fall back to Source)", svc.Parish)
	}
}

func TestMapToServiceLanguageFallback(t *testing.T) {
	m := map[string]interface{}{
		"source":       "Test",
		"date":         "2026-03-08",
		"service_name": "Liturgi",
		"language":     "Svenska",
	}

	svc, err := mapToService(m)
	if err != nil {
		t.Fatalf("mapToService: %v", err)
	}

	if svc.ParishLanguage == nil || *svc.ParishLanguage != "Svenska" {
		t.Errorf("ParishLanguage = %v, want %q (should fall back to Language)", svc.ParishLanguage, "Svenska")
	}
}

func TestMapToServiceStartEndTime(t *testing.T) {
	m := map[string]interface{}{
		"source":       "Test",
		"date":         "2026-03-08",
		"service_name": "Liturgi",
		"start_time":   "2026-03-08T10:00:00Z",
		"end_time":     "2026-03-08T11:00:00Z",
	}

	svc, err := mapToService(m)
	if err != nil {
		t.Fatalf("mapToService: %v", err)
	}

	if svc.StartTime == nil {
		t.Fatal("StartTime should be parsed")
	}
	if svc.StartTime.Hour() != 10 {
		t.Errorf("StartTime hour = %d, want 10", svc.StartTime.Hour())
	}
	if svc.EndTime == nil {
		t.Fatal("EndTime should be parsed")
	}
	if svc.EndTime.Hour() != 11 {
		t.Errorf("EndTime hour = %d, want 11", svc.EndTime.Hour())
	}
}

func TestServiceToMapOmitsEmptyOptionals(t *testing.T) {
	svc := model.ChurchService{
		Parish:      "Test",
		Source:      "Test",
		Date:        "2026-03-08",
		ServiceName: "Liturgi",
	}

	m := serviceToMap(svc, "scraper", "batch")

	for _, key := range []string{"title", "source_url", "location", "time", "occasion", "notes", "language", "parish_language", "event_language", "start_time", "end_time"} {
		if _, ok := m[key]; ok {
			t.Errorf("map should not contain %q for zero-value service", key)
		}
	}
}
