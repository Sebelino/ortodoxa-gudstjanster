package scraper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ortodoxa-gudstjanster/internal/store"
	"ortodoxa-gudstjanster/internal/vision"
)

func TestDemetriosResolveDate(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	date, wd := demetriosResolveDate(18, 10, now)
	if date != "2026-10-18" || wd != "Söndag" {
		t.Errorf("got (%q, %q), want (2026-10-18, Söndag)", date, wd)
	}
	// Late December, looking at a January line → next year.
	now = time.Date(2026, 12, 20, 12, 0, 0, 0, time.UTC)
	if date, _ := demetriosResolveDate(7, 1, now); date != "2027-01-07" {
		t.Errorf("got %q, want 2027-01-07", date)
	}
	// Mid January, looking at a December line → previous year.
	now = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	if date, _ := demetriosResolveDate(26, 12, now); date != "2025-12-26" {
		t.Errorf("got %q, want 2025-12-26", date)
	}
}

const (
	demetriosLineSunday = "Söndag 18 okt kl 10:00 Gudomlig Liturgi i Gode Herdens kapell i Brickebackens centrum med start 09:00(bikt fram till 09:30) Orthros. Fika efteråt, välkomna"
	demetriosLineCancel = "Söndag 25 ok: Ingen Liturgi på Söndag, då jag är i Stockholm"
)

func TestValidateDemetriosLine(t *testing.T) {
	good := &vision.LineSchedule{Days: []vision.LineDay{{Day: 18, Month: 10, Services: []vision.LineService{
		{Time: "10:00", Name: demetriosLineSunday[len("Söndag 18 okt "):]},
		{Time: "09:00", Name: "start 09:00(bikt fram till 09:30) Orthros", IsOrthros: true},
	}}}}
	if err := validateDemetriosLine(demetriosLineSunday, good); err != nil {
		t.Errorf("valid parse rejected: %v", err)
	}
	cancel := &vision.LineSchedule{Days: []vision.LineDay{{Day: 25, Month: 10}}}
	if err := validateDemetriosLine(demetriosLineCancel, cancel); err != nil {
		t.Errorf("cancellation rejected: %v", err)
	}

	bad := map[string]*vision.LineSchedule{
		"wrong month":      {Days: []vision.LineDay{{Day: 18, Month: 11, Services: good.Days[0].Services[:1]}}},
		"day not in line":  {Days: []vision.LineDay{{Day: 19, Month: 10, Services: good.Days[0].Services[:1]}}},
		"hour not in line": {Days: []vision.LineDay{{Day: 18, Month: 10, Services: []vision.LineService{{Time: "17:00", Name: "Gudomlig Liturgi"}}}}},
		"malformed time":   {Days: []vision.LineDay{{Day: 18, Month: 10, Services: []vision.LineService{{Time: "10", Name: "Gudomlig Liturgi"}}}}},
		"translated name":  {Days: []vision.LineDay{{Day: 18, Month: 10, Services: []vision.LineService{{Time: "10:00", Name: "Divine Liturgy at 10:00"}}}}},
	}
	for name, ls := range bad {
		if err := validateDemetriosLine(demetriosLineSunday, ls); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

type fakeLineParser struct {
	calls  int
	result *vision.LineSchedule
	err    error
}

func (f *fakeLineParser) ParseScheduleLine(context.Context, string) (*vision.LineSchedule, error) {
	f.calls++
	return f.result, f.err
}

func TestDemetriosReadLineCaches(t *testing.T) {
	st, err := store.NewLocal(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeLineParser{result: &vision.LineSchedule{Days: []vision.LineDay{{Day: 25, Month: 10}}}}
	s := &DemetriosScraper{store: st, parser: fake}

	for i, wantHit := range []bool{false, true} {
		got, hit, err := s.readLine(context.Background(), demetriosLineCancel)
		if err != nil || hit != wantHit || len(got.Days) != 1 {
			t.Fatalf("call %d: got (%v, hit=%v, %v), want hit=%v", i, got, hit, err, wantHit)
		}
	}
	if fake.calls != 1 {
		t.Errorf("parser called %d times, want 1 (second read should hit the cache)", fake.calls)
	}
}

func TestDemetriosReadLineRejectsInvalidAndDoesNotCache(t *testing.T) {
	st, _ := store.NewLocal(filepath.Join(t.TempDir(), "store"))
	fake := &fakeLineParser{result: &vision.LineSchedule{Days: []vision.LineDay{{Day: 3, Month: 2}}}}
	s := &DemetriosScraper{store: st, parser: fake}

	if _, _, err := s.readLine(context.Background(), demetriosLineCancel); err == nil {
		t.Fatal("expected validation error")
	}
	fake.result = &vision.LineSchedule{Days: []vision.LineDay{{Day: 25, Month: 10}}}
	if _, hit, err := s.readLine(context.Background(), demetriosLineCancel); err != nil || hit {
		t.Errorf("after a rejected parse, got (hit=%v, %v), want fresh successful read", hit, err)
	}

	fake.err = errors.New("boom")
	s2 := &DemetriosScraper{parser: fake}
	if _, _, err := s2.readLine(context.Background(), demetriosLineSunday); err == nil {
		t.Error("expected parser error to propagate")
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

func TestDemetriosService(t *testing.T) {
	// Booking/contact text stays in the Beskrivning verbatim; only the trailing "." goes.
	svc := demetriosService("2026-09-05", "Lördag", vision.LineService{
		Time: "11:00", Name: "Bikt vid kl:11 meddela/boka på gerav23@gmail.com innan."})
	if svc.ServiceName != "Bikt vid kl:11 meddela/boka på gerav23@gmail.com innan" {
		t.Errorf("ServiceName = %q", svc.ServiceName)
	}
	if svc.Time == nil || *svc.Time != "11:00" || svc.Title != "" || svc.Notes != nil {
		t.Errorf("unexpected service: %+v", svc)
	}

	svc = demetriosService("2026-10-18", "Söndag", vision.LineService{
		Time: "09:00", Name: "Orhros kl 09:00 i Brickebacken", IsOrthros: true})
	if svc.Title != "Orthros" {
		t.Errorf("Title = %q, want Orthros", svc.Title)
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

	deps := newTestDeps(t, filepath.Join(os.TempDir(), "ortodoxa-gudstjanster-store-test"))
	scraper := NewDemetriosScraper(deps.store, deps.vision)
	services, err := scraper.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	for _, s := range services {
		validateService(t, s, scraper.Name())
	}
	assertHasCurrentMonthEvents(t, services)
}
