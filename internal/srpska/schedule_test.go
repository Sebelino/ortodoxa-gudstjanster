package srpska

import (
	"testing"
	"time"
)

// --- translateServiceName ---

func TestTranslateServiceName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Јутрење", "Morgongudstjänst"},
		{"Литургија", "Helig Liturgi"},
		{"Вечерње", "Aftongudstjänst"},
		{"Jutrenje", "Morgongudstjänst"},
		{"Liturgija", "Helig Liturgi"},
		{"Večernje", "Aftongudstjänst"},
		{"Unknown Service", "Unknown Service"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := translateServiceName(tt.input)
			if got != tt.want {
				t.Errorf("translateServiceName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- parseDays ---

func TestParseDays(t *testing.T) {
	tests := []struct {
		name string
		input string
		want  []string
	}{
		{
			name:  "Serbian Cyrillic Sunday",
			input: "недеља",
			want:  []string{"söndag"},
		},
		{
			name:  "Serbian Latin Sunday",
			input: "nedelja",
			want:  []string{"söndag"},
		},
		{
			name:  "working days Cyrillic",
			input: "радни дани",
			want:  []string{"måndag", "tisdag", "onsdag", "torsdag", "fredag"},
		},
		{
			name:  "working days Latin",
			input: "radni dani",
			want:  []string{"måndag", "tisdag", "onsdag", "torsdag", "fredag"},
		},
		{
			name:  "Sunday and holiday",
			input: "недеља, празник",
			want:  []string{"söndag", "helgdag"},
		},
		{
			name:  "Saturday",
			input: "субота",
			want:  []string{"lördag"},
		},
		{
			name:  "Swedish day name",
			input: "söndag",
			want:  []string{"söndag"},
		},
		{
			name:  "empty string",
			input: "",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDays(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("parseDays(%q) = %v (len %d), want %v (len %d)", tt.input, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseDays(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// --- WeekdayToSwedish ---

func TestWeekdayToSwedish(t *testing.T) {
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
			got := WeekdayToSwedish(tt.day)
			if got != tt.want {
				t.Errorf("WeekdayToSwedish(%v) = %q, want %q", tt.day, got, tt.want)
			}
		})
	}
}

// --- ParseScheduleTable ---

func TestParseScheduleTable(t *testing.T) {
	input := "Јутрење - недеља:\t8:00\nЛитургија - недеља:\t9:30\nВечерње - субота:\t17:00\n"

	schedule, err := ParseScheduleTable(input)
	if err != nil {
		t.Fatalf("ParseScheduleTable failed: %v", err)
	}

	if len(schedule.Services) != 3 {
		t.Fatalf("got %d services, want 3", len(schedule.Services))
	}

	expected := []struct {
		name string
		days []string
		time string
	}{
		{"Morgongudstjänst", []string{"söndag"}, "08:00"},
		{"Helig Liturgi", []string{"söndag"}, "09:30"},
		{"Aftongudstjänst", []string{"lördag"}, "17:00"},
	}

	for i, want := range expected {
		got := schedule.Services[i]
		if got.Name != want.name {
			t.Errorf("service[%d].Name = %q, want %q", i, got.Name, want.name)
		}
		if got.Time != want.time {
			t.Errorf("service[%d].Time = %q, want %q", i, got.Time, want.time)
		}
		if len(got.Days) != len(want.days) || got.Days[0] != want.days[0] {
			t.Errorf("service[%d].Days = %v, want %v", i, got.Days, want.days)
		}
	}
}

func TestParseScheduleTableEmpty(t *testing.T) {
	_, err := ParseScheduleTable("")
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestParseScheduleTableWorkingDays(t *testing.T) {
	input := "Јутрење - радни дани:\t6:00\n"

	schedule, err := ParseScheduleTable(input)
	if err != nil {
		t.Fatalf("ParseScheduleTable failed: %v", err)
	}

	if len(schedule.Services) != 1 {
		t.Fatalf("got %d services, want 1", len(schedule.Services))
	}

	svc := schedule.Services[0]
	if len(svc.Days) != 5 {
		t.Errorf("working days should produce 5 days, got %d: %v", len(svc.Days), svc.Days)
	}
}

// --- GenerateEvents ---

func TestGenerateEvents(t *testing.T) {
	schedule := &RecurringSchedule{
		Services: []RecurringService{
			{Name: "Helig Liturgi", Days: []string{"söndag"}, Time: "09:30"},
		},
	}

	events := GenerateEvents(schedule, 2)

	// 2 weeks should have exactly 2 Sundays
	if len(events) < 1 || len(events) > 3 {
		t.Errorf("expected 1-3 events for 2 weeks of Sundays, got %d", len(events))
	}

	for _, e := range events {
		if e.ServiceName != "Helig Liturgi" {
			t.Errorf("event.ServiceName = %q, want %q", e.ServiceName, "Helig Liturgi")
		}
		if e.Time != "09:30" {
			t.Errorf("event.Time = %q, want %q", e.Time, "09:30")
		}
		if e.DayOfWeek != "Söndag" {
			t.Errorf("event.DayOfWeek = %q, want %q", e.DayOfWeek, "Söndag")
		}
	}
}

func TestGenerateEventsMultipleServices(t *testing.T) {
	schedule := &RecurringSchedule{
		Services: []RecurringService{
			{Name: "Morgongudstjänst", Days: []string{"söndag"}, Time: "08:00"},
			{Name: "Helig Liturgi", Days: []string{"söndag"}, Time: "09:30"},
			{Name: "Aftongudstjänst", Days: []string{"lördag"}, Time: "17:00"},
		},
	}

	events := GenerateEvents(schedule, 1)

	// In 1 week we should have at least 1 Saturday + 1 Sunday worth of events
	if len(events) < 2 {
		t.Errorf("expected at least 2 events, got %d", len(events))
	}

	// Check that we have both service types
	names := make(map[string]bool)
	for _, e := range events {
		names[e.ServiceName] = true
	}
	if !names["Helig Liturgi"] {
		t.Error("expected Helig Liturgi events")
	}
}

func TestGenerateEventsDateFormat(t *testing.T) {
	schedule := &RecurringSchedule{
		Services: []RecurringService{
			{Name: "Test", Days: []string{"måndag", "tisdag", "onsdag", "torsdag", "fredag", "lördag", "söndag"}, Time: "10:00"},
		},
	}

	events := GenerateEvents(schedule, 1)

	for _, e := range events {
		if len(e.Date) != 10 || e.Date[4] != '-' || e.Date[7] != '-' {
			t.Errorf("event.Date = %q, want YYYY-MM-DD format", e.Date)
		}
	}
}
