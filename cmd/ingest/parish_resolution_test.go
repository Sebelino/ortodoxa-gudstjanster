package main

import (
	"testing"

	"ortodoxa-gudstjanster/internal/model"
)

var testSlugToName = map[string]string{
	"helige-sergij": "Helige Sergij rysk-ortodoxa församling",
	"heliga-anna":   "Heliga Anna av Novgorod",
	"st-georgios":   "St. Georgios Cathedral",
}

var testNameToSlug = map[string]string{
	"Helige Sergij rysk-ortodoxa församling": "helige-sergij",
	"Heliga Anna av Novgorod":                "heliga-anna",
	"St. Georgios Cathedral":                 "st-georgios",
}

func TestResolveParishFields_SlugToName(t *testing.T) {
	svc := model.ChurchService{ParishSlug: "helige-sergij"}
	unknown := resolveParishFields(&svc, "Helige Sergij rysk-ortodoxa församling", testSlugToName, testNameToSlug)
	if svc.Parish != "Helige Sergij rysk-ortodoxa församling" {
		t.Errorf("Parish = %q, want %q", svc.Parish, "Helige Sergij rysk-ortodoxa församling")
	}
	if svc.ParishSlug != "helige-sergij" {
		t.Errorf("ParishSlug changed to %q", svc.ParishSlug)
	}
	if unknown {
		t.Error("should not report unknown for a known slug")
	}
}

func TestResolveParishFields_NameToSlug(t *testing.T) {
	svc := model.ChurchService{Parish: "Heliga Anna av Novgorod"}
	unknown := resolveParishFields(&svc, "Google Calendar (Heliga Anna / St. Ignatios)", testSlugToName, testNameToSlug)
	if svc.ParishSlug != "heliga-anna" {
		t.Errorf("ParishSlug = %q, want %q", svc.ParishSlug, "heliga-anna")
	}
	if svc.Parish != "Heliga Anna av Novgorod" {
		t.Errorf("Parish changed to %q", svc.Parish)
	}
	if unknown {
		t.Error("name-to-slug resolution should not report unknown")
	}
}

func TestResolveParishFields_BothSet_NoOverwrite(t *testing.T) {
	svc := model.ChurchService{
		Parish:     "St. Georgios Cathedral",
		ParishSlug: "st-georgios",
	}
	unknown := resolveParishFields(&svc, "St. Georgios Cathedral", testSlugToName, testNameToSlug)
	if svc.Parish != "St. Georgios Cathedral" {
		t.Errorf("Parish changed to %q", svc.Parish)
	}
	if svc.ParishSlug != "st-georgios" {
		t.Errorf("ParishSlug changed to %q", svc.ParishSlug)
	}
	if unknown {
		t.Error("both-set case should not report unknown")
	}
}

func TestResolveParishFields_UnknownSlug_WithData_ReportsUnknown(t *testing.T) {
	svc := model.ChurchService{ParishSlug: "nonexistent-parish"}
	unknown := resolveParishFields(&svc, "Canonical Parish Name", testSlugToName, testNameToSlug)
	if !unknown {
		t.Error("should report unknown when slug not in uMap and uMap data is available")
	}
	if svc.Parish != "Canonical Parish Name" {
		t.Errorf("Parish = %q, want scraper name as fallback", svc.Parish)
	}
}

func TestResolveParishFields_UnknownName(t *testing.T) {
	svc := model.ChurchService{Parish: "Okänd Församling"}
	unknown := resolveParishFields(&svc, "Okänd Församling", testSlugToName, testNameToSlug)
	if svc.ParishSlug != "" {
		t.Errorf("ParishSlug = %q, want empty for unknown name", svc.ParishSlug)
	}
	if unknown {
		t.Error("name-to-slug miss should not report unknown")
	}
}

func TestResolveParishFields_BothEmpty_NoOp(t *testing.T) {
	svc := model.ChurchService{ServiceName: "Gudomlig Liturgi"}
	unknown := resolveParishFields(&svc, "Some Scraper", testSlugToName, testNameToSlug)
	if svc.Parish != "" || svc.ParishSlug != "" {
		t.Errorf("expected no changes for empty Parish and ParishSlug")
	}
	if unknown {
		t.Error("both-empty case should not report unknown")
	}
}

func TestResolveParishFields_EmptyMaps_FallsBackToScraperName_NoAlert(t *testing.T) {
	svc := model.ChurchService{ParishSlug: "helige-sergij"}
	unknown := resolveParishFields(&svc, "Helige Sergij rysk-ortodoxa församling", map[string]string{}, map[string]string{})
	if svc.Parish != "Helige Sergij rysk-ortodoxa församling" {
		t.Errorf("Parish = %q, want scraper name as fallback when maps are empty", svc.Parish)
	}
	if unknown {
		t.Error("should not report unknown when uMap data is unavailable (empty maps)")
	}
}
