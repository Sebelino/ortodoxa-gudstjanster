package main

import (
	"testing"
	"time"

	"ortodoxa-gudstjanster/internal/model"
)

func TestApplyCorrectionsMovesDate(t *testing.T) {
	serviceTime := "10:00"
	start := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	accepted := []acceptedResult{{services: []model.ChurchService{
		{ParishSlug: "demetrios", Date: "2026-11-02", DayOfWeek: "Måndag", Time: &serviceTime, StartTime: &start},
		{ParishSlug: "demetrios", Date: "2026-11-08", DayOfWeek: "Söndag", Time: &serviceTime},
	}}}
	corrections := []model.Correction{{
		ParishSlug: "demetrios", Date: "2026-11-02", OriginalTime: "10:00", NewDate: "2026-11-01", Reason: "Fel datum",
	}}

	if applied := applyCorrections(accepted, corrections); applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}
	moved := accepted[0].services[0]
	if moved.Date != "2026-11-01" || moved.DayOfWeek != "Söndag" {
		t.Errorf("moved to %s %s, want 2026-11-01 Söndag", moved.Date, moved.DayOfWeek)
	}
	if want := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC); !moved.StartTime.Equal(want) {
		t.Errorf("StartTime = %v, want %v", moved.StartTime, want)
	}
	if moved.Correction == nil || *moved.Correction != "Fel datum" {
		t.Errorf("Correction = %v, want the reason", moved.Correction)
	}
	if other := accepted[0].services[1]; other.Date != "2026-11-08" {
		t.Errorf("unrelated service moved to %s", other.Date)
	}
}

func TestApplyCorrectionsInvalidNewDateKeepsService(t *testing.T) {
	serviceTime := "10:00"
	accepted := []acceptedResult{{services: []model.ChurchService{
		{ParishSlug: "demetrios", Date: "2026-11-02", Time: &serviceTime},
	}}}
	corrections := []model.Correction{{
		ParishSlug: "demetrios", Date: "2026-11-02", OriginalTime: "10:00", NewDate: "1 november", Reason: "Fel datum",
	}}

	applyCorrections(accepted, corrections)
	if got := accepted[0].services; len(got) != 1 || got[0].Date != "2026-11-02" || got[0].Correction != nil {
		t.Errorf("service changed by an invalid correction: %+v", got)
	}
}
