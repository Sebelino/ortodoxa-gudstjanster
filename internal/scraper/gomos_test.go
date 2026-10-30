package scraper

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"ortodoxa-gudstjanster/internal/vision"
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

// TestFixSundayOrdinalsRealAugust2026Misread reproduces a real production bug: OpenAI's
// vision OCR misread the Greek ordinal "ΙΓ'" (13) as "ΙΒ'" (12) on the Aug 30, 2026 entry,
// which the translation step faithfully (and wrongly) rendered as "Tolfte" — duplicating
// the number already used for a different Sunday. fixSundayOrdinals must recompute it from
// the date instead of trusting the OCR'd numeral.
func TestFixSundayOrdinalsRealAugust2026Misread(t *testing.T) {
	entries := []vision.ScheduleEntry{
		{Date: "2026-08-02", Occasion: "Nionde söndagen i Matteus"},
		{Date: "2026-08-09", Occasion: "Tionde söndagen i Matteus"},
		{Date: "2026-08-16", Occasion: "Elfte söndagen i Matteus"},
		{Date: "2026-08-23", Occasion: "Avslutningen av högtiden för Guds moders insomnande"},
		{Date: "2026-08-30", Occasion: "Tolfte söndagen i Matteus"}, // misread; should be 13th
	}
	fixSundayOrdinals(entries)

	want := []string{
		"Nionde söndagen i Matteus",
		"Tionde söndagen i Matteus",
		"Elfte söndagen i Matteus",
		"Avslutningen av högtiden för Guds moders insomnande",
		"Trettonde söndagen i Matteus",
	}
	for i, w := range want {
		if entries[i].Occasion != w {
			t.Errorf("entry %d (%s) occasion = %q, want %q", i, entries[i].Date, entries[i].Occasion, w)
		}
	}
}

func TestFixSundayOrdinalsIndependentEvangelistGroups(t *testing.T) {
	entries := []vision.ScheduleEntry{
		{Date: "2026-08-02", Occasion: "Nionde söndagen i Matteus"},
		{Date: "2026-08-09", Occasion: "Tionde söndagen i Matteus"},
		{Date: "2026-11-01", Occasion: "Andra söndagen i Lukas"},
		{Date: "2026-11-08", Occasion: "Tredje söndagen i Lukas"},
	}
	fixSundayOrdinals(entries)

	want := []string{
		"Nionde söndagen i Matteus",
		"Tionde söndagen i Matteus",
		"Andra söndagen i Lukas",
		"Tredje söndagen i Lukas",
	}
	for i, w := range want {
		if entries[i].Occasion != w {
			t.Errorf("entry %d occasion = %q, want %q", i, entries[i].Occasion, w)
		}
	}
}

// The real October 2026 schedule prints Β', Δ' and ΣΤ' Λουκά, with the feast of St. Luke
// in between. The printed numbers skip ahead of the calendar and must be kept.
func TestFixSundayOrdinalsKeepsLectionaryJump(t *testing.T) {
	entries := []vision.ScheduleEntry{
		{Date: "2026-10-04", Occasion: "Andra söndagen i Lukas"},
		{Date: "2026-10-11", Occasion: "Fjärde söndagen i Lukas"},
		{Date: "2026-10-18", Occasion: "Lukas, evangelisten"},
		{Date: "2026-10-25", Occasion: "Sjätte söndagen i Lukas"},
	}
	want := []string{"Andra söndagen i Lukas", "Fjärde söndagen i Lukas", "Lukas, evangelisten", "Sjätte söndagen i Lukas"}
	fixSundayOrdinals(entries)
	for i, w := range want {
		if entries[i].Occasion != w {
			t.Errorf("entry %d (%s) occasion = %q, want %q", i, entries[i].Date, entries[i].Occasion, w)
		}
	}
}

// A misread after the jump is still corrected, counting from the jumped number.
func TestFixSundayOrdinalsCorrectsMisreadAfterJump(t *testing.T) {
	entries := []vision.ScheduleEntry{
		{Date: "2026-10-04", Occasion: "Andra söndagen i Lukas"},
		{Date: "2026-10-11", Occasion: "Fjärde söndagen i Lukas"},
		{Date: "2026-10-25", Occasion: "Femte söndagen i Lukas"},
	}
	fixSundayOrdinals(entries)
	if got, want := entries[2].Occasion, "Sjätte söndagen i Lukas"; got != want {
		t.Errorf("occasion = %q, want %q", got, want)
	}
}

func TestFixSundayOrdinalsLeavesNonOrdinalOccasionsUntouched(t *testing.T) {
	entries := []vision.ScheduleEntry{
		{Date: "2026-08-27", Occasion: "Den helige storemartyr Fanourios"},
		{Date: "2026-08-06", Occasion: "Kristi Förklaring"},
	}
	fixSundayOrdinals(entries)

	if entries[0].Occasion != "Den helige storemartyr Fanourios" {
		t.Errorf("non-ordinal occasion was modified: %q", entries[0].Occasion)
	}
	if entries[1].Occasion != "Kristi Förklaring" {
		t.Errorf("non-ordinal occasion was modified: %q", entries[1].Occasion)
	}
}

// TestFixArchbishopTitle covers the real bug (the translation model consistently
// mistransliterates "Cleopas" as "Kleopas" — confirmed across production translation runs
// from April to August 2026, regardless of what the prompt says) and the honorific-class
// slips seen alongside it, while confirming the gating added after that model was fixed
// does NOT blindly overwrite an unrelated, correctly-translated name that merely shares
// the "av Sverige"/"av Elaia" suffix pattern.
func TestFixArchbishopTitle(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			"real bug: Kleopas mistransliteration",
			"Gudomlig Liturgi, med Hans Eminens Ärkebiskop Kleopas av Sverige",
			"Gudomlig Liturgi, med Hans Eminens Ärkebiskop Cleopas av Sverige",
		},
		{
			"real bug: wrong honorific class and missing Ärkebiskop",
			"Gudomlig Liturgi, med Hans Nåd Cleopas av Sverige",
			"Gudomlig Liturgi, med Hans Eminens Ärkebiskop Cleopas av Sverige",
		},
		{
			"Bartolomaios Latinized form",
			"Gudomlig Liturgi, med Hans Nåd Bartholomew av Elaia",
			"Gudomlig Liturgi, med Hans Nåd Bartolomaios av Elaia",
		},
		{
			"older official spelling with h",
			"Gudomlig Liturgi, med Hans Nåd Bartholomaios av Elaia",
			"Gudomlig Liturgi, med Hans Nåd Bartolomaios av Elaia",
		},
		{
			"real bug: Αρχ. read as Archimandrite",
			"Ärkeprästerlig Gudomlig Liturgi, med Hans Nåd Arkimandrit Bartholomaios",
			"Ärkeprästerlig Gudomlig Liturgi, med Hans Nåd Bartolomaios av Elaia",
		},
		{
			"unrelated hierarch styled av Sverige is not overwritten",
			"Gudomlig Liturgi, med Hans Eminens Ärkebiskop Nikodemos av Sverige",
			"Gudomlig Liturgi, med Hans Eminens Ärkebiskop Nikodemos av Sverige",
		},
		{
			"unrelated hierarch styled av Elaia is not overwritten",
			"Gudomlig Liturgi, med Hans Nåd Ioakeim av Elaia",
			"Gudomlig Liturgi, med Hans Nåd Ioakeim av Elaia",
		},
		{
			"entry with no honorific is untouched",
			"Paraklesis till Guds moder",
			"Paraklesis till Guds moder",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries := []vision.ScheduleEntry{{ServiceName: tt.in}}
			fixArchbishopTitle(entries)
			if entries[0].ServiceName != tt.want {
				t.Errorf("ServiceName = %q, want %q", entries[0].ServiceName, tt.want)
			}
		})
	}
}

func TestFixOfficialTerms(t *testing.T) {
	tests := []struct{ in, want string }{
		{
			"Ärkepåstterlig Gudomlig Liturgi, Metropoliten Bartholomaios av Polyani och Kilkis",
			"Ärkeprästerlig Gudomlig Liturgi, Metropoliten Bartholomaios av Polyani och Kilkis",
		},
		{"Hierarkisk Gudomlig Liturgi, med Hans Eminens Ärkebiskop Cleopas", "Ärkeprästerlig Gudomlig Liturgi, med Hans Eminens Ärkebiskop Cleopas"},
		{"Föreläsning för katekumerer, med Hans Eminens Cleopas av Sverige på engelska", "Föreläsning för katekumener, med Hans Eminens Cleopas av Sverige på engelska"},
		{
			"Ärkeprästerlig Gudomlig Liturgi, med Hans Eminens Ärkebiskop Cleopas av Sverige, för firandet av den helige Nikolaj i Stockholm till ära för martyrerna i Karelen",
			"Ärkeprästerlig Gudomlig Liturgi, med Hans Eminens Ärkebiskop Cleopas av Sverige, i Helige Nikolai församling i Stockholm, för högtiden för Karelens helgon",
		},
		{"Ärkeprästerlig Gudomlig Liturgi", "Ärkeprästerlig Gudomlig Liturgi"},
		{"Gudomlig Liturgi", "Gudomlig Liturgi"},
	}
	for _, tt := range tests {
		entries := []vision.ScheduleEntry{{ServiceName: tt.in}}
		fixOfficialTerms(entries)
		if entries[0].ServiceName != tt.want {
			t.Errorf("fixOfficialTerms(%q) = %q, want %q", tt.in, entries[0].ServiceName, tt.want)
		}
	}
}

func TestNormalizeKnownParishLocationNikolaj(t *testing.T) {
	got := normalizeKnownParishLocation("Församlingen av den helige Nikolaj i Stockholm")
	if want := "Bellmansgatan 13, 118 47 Stockholm"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
