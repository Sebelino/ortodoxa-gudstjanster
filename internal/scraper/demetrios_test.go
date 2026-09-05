package scraper

import (
	"context"
	"testing"
	"time"
)

// demetriosTestNow is a fixed "now" that puts a September schedule in the current year.
var demetriosTestNow = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func TestParseDemetriosLine(t *testing.T) {
	tests := []struct {
		name                         string
		in                           string
		wantOK                       bool
		wantDate, wantWD, wantClause string
	}{
		{
			"confession line",
			"Lördag 5:e September: Bikt vid kl:11 meddela/boka på gerav23@gmail.com innan",
			true, "2026-09-05", "Lördag", "Bikt vid kl:11 meddela/boka på gerav23@gmail.com innan",
		},
		{
			"two-service sunday line, trailing period",
			"Söndag 6:e September: Liturgi 10:00 och Orhros kl 09:00 i Brickebacken.",
			true, "2026-09-06", "Söndag", "Liturgi 10:00 och Orhros kl 09:00 i Brickebacken.",
		},
		{
			"weekday recomputed from date (source label ignored)",
			"Måndag 6:e September: Vesper 18:00",
			true, "2026-09-06", "Söndag", "Vesper 18:00",
		},
		{"saint heading, no date shape", "5 sep: Helige Profeten Zacharias", false, "", "", ""},
		{"quote line", "Johannes Chrysostomos (ca 347–407)", false, "", "", ""},
		{"prefix only, empty clause", "Lördag 5:e September:", false, "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			date, wd, clause, ok := parseDemetriosLine(tt.in, demetriosTestNow)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if date != tt.wantDate || wd != tt.wantWD || clause != tt.wantClause {
				t.Errorf("got (%q, %q, %q), want (%q, %q, %q)", date, wd, clause, tt.wantDate, tt.wantWD, tt.wantClause)
			}
		})
	}
}

func TestParseDemetriosLineYearWindow(t *testing.T) {
	// Late December, looking at a January line → next year.
	now := time.Date(2026, 12, 20, 12, 0, 0, 0, time.UTC)
	date, _, _, ok := parseDemetriosLine("Torsdag 7:e Januari: Liturgi 10:00", now)
	if !ok || date != "2027-01-07" {
		t.Errorf("got (%q, %v), want 2027-01-07", date, ok)
	}
	// Mid January, looking at a December line → previous year.
	now = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	date, _, _, ok = parseDemetriosLine("Lördag 26:e December: Vesper 17:00", now)
	if !ok || date != "2025-12-26" {
		t.Errorf("got (%q, %v), want 2025-12-26", date, ok)
	}
}

func TestSplitDemetriosClause(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{
			"Liturgi 10:00 och Orhros kl 09:00 i Brickebacken",
			[]string{"Liturgi 10:00", "Orhros kl 09:00 i Brickebacken"},
		},
		{ // second half has no time → not split
			"17:00 Esperinos och Ikon&Koboskini tillverkning samt bikt",
			[]string{"17:00 Esperinos och Ikon&Koboskini tillverkning samt bikt"},
		},
		{ // single service, no connector
			"Bikt vid kl:11 meddela/boka på gerav23@gmail.com innan",
			[]string{"Bikt vid kl:11 meddela/boka på gerav23@gmail.com innan"},
		},
		{ // comma-separated, both timed
			"Julotta kl 07:00, Liturgi kl 10:00",
			[]string{"Julotta kl 07:00", "Liturgi kl 10:00"},
		},
	}
	for _, tt := range tests {
		got := splitDemetriosClause(tt.in)
		if len(got) != len(tt.want) {
			t.Errorf("split(%q) = %q, want %q", tt.in, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("split(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
			}
		}
	}
}

func TestDemetriosTime(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Bikt vid kl:11 meddela/boka på gerav23@gmail.com innan", "11:00"},
		{"Orhros kl 09:00 i Brickebacken", "09:00"},
		{"Liturgi 10:00", "10:00"},
		{"17:00 Esperinos", "17:00"},
		{"Bikt kl:11-12, boka/meddela f.Georgios", "11:00"},
		{"kl. 9.00 Morgongudstjänst", "09:00"},
		{"Ingen tid alls här", ""},
		{"mejla till a23@b.se", ""}, // digits in an email are not a time
	}
	for _, tt := range tests {
		if got := demetriosTime(tt.in); got != tt.want {
			t.Errorf("demetriosTime(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDemetriosSegmentToService(t *testing.T) {
	// Booking/contact text stays in the Beskrivning verbatim — the whole clause,
	// only the leading date prefix (handled upstream) and trailing "." removed.
	svc, ok := demetriosSegmentToService("2026-09-05", "Lördag",
		"Bikt vid kl:11 meddela/boka på gerav23@gmail.com innan")
	if !ok {
		t.Fatal("expected a service")
	}
	if svc.ServiceName != "Bikt vid kl:11 meddela/boka på gerav23@gmail.com innan" {
		t.Errorf("ServiceName = %q", svc.ServiceName)
	}
	if svc.Time == nil || *svc.Time != "11:00" {
		t.Errorf("Time = %v, want 11:00", svc.Time)
	}
	if svc.Notes != nil {
		t.Errorf("Notes = %v, want nil (kept in Beskrivning instead)", *svc.Notes)
	}
	if svc.Title != "" {
		t.Errorf("Title = %q, want empty", svc.Title)
	}

	// Orthros segment: verbatim name, explicit title override.
	svc, ok = demetriosSegmentToService("2026-09-06", "Söndag", "Orhros kl 09:00 i Brickebacken.")
	if !ok {
		t.Fatal("expected a service")
	}
	if svc.ServiceName != "Orhros kl 09:00 i Brickebacken" { // trailing "." trimmed
		t.Errorf("ServiceName = %q", svc.ServiceName)
	}
	if svc.Title != "Orthros" {
		t.Errorf("Title = %q, want Orthros", svc.Title)
	}

	// A segment with no time is not a service.
	if _, ok := demetriosSegmentToService("2026-09-06", "Söndag", "bara text"); ok {
		t.Error("expected no service for a segment without a time")
	}
}

func TestLooksLikeDemetriosSchedule(t *testing.T) {
	yes := []string{
		"Söndag 6 september – Liturgi kl 10", // en-dash instead of ":"
		"6/9: Liturgi 10:00",                 // numeric date
		"Den 6 September: Orhros kl 09:00",   // "Den" prefix
		"Vesper 18:00",                       // bare time, no date
	}
	no := []string{
		"Johannes Chrysostomos (ca 347–407)",
		"Den outgrudlige, Andrén, Artos",
		"5 sep: Helige Profeten Zacharias", // abbreviated month, no time
		"",
	}
	for _, s := range yes {
		if !looksLikeDemetriosSchedule(s) {
			t.Errorf("looksLikeDemetriosSchedule(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if looksLikeDemetriosSchedule(s) {
			t.Errorf("looksLikeDemetriosSchedule(%q) = true, want false", s)
		}
	}
}

func TestDemetriosScraperHasCurrentMonthEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	scraper := NewDemetriosScraper()
	services, err := scraper.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	for _, s := range services {
		validateService(t, s, scraper.Name())
	}
	assertHasCurrentMonthEvents(t, services)
}
