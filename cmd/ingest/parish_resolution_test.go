package main

import (
	"testing"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/umap"
)

var testSlugToParish = map[string]umap.Parish{
	"helige-sergij": {Name: "Helige Sergij rysk-ortodoxa församling", PrimaryLanguage: "Kyrkoslaviska"},
	"heliga-anna":   {Name: "Heliga Anna av Novgorod", PrimaryLanguage: "Svenska"},
	"st-georgios":   {Name: "St. Georgios Cathedral", PrimaryLanguage: "Grekiska", SecondaryLanguages: []string{"Svenska", "Engelska"}},
}

var testNameToSlug = map[string]string{
	"Helige Sergij rysk-ortodoxa församling": "helige-sergij",
	"Heliga Anna av Novgorod":                "heliga-anna",
	"St. Georgios Cathedral":                 "st-georgios",
}

func TestResolveParishFields_SlugToName(t *testing.T) {
	svc := model.ChurchService{ParishSlug: "helige-sergij"}
	unknown := resolveParishFields(&svc, "Helige Sergij rysk-ortodoxa församling", testSlugToParish, testNameToSlug)
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

func TestResolveParishFields_SlugToName_SetsLanguage(t *testing.T) {
	svc := model.ChurchService{ParishSlug: "st-georgios"}
	resolveParishFields(&svc, "St. Georgios Cathedral", testSlugToParish, testNameToSlug)
	if svc.ParishLanguage == nil {
		t.Fatal("ParishLanguage should be set from uMap")
	}
	if *svc.ParishLanguage != "Grekiska, Svenska, Engelska" {
		t.Errorf("ParishLanguage = %q, want %q", *svc.ParishLanguage, "Grekiska, Svenska, Engelska")
	}
}

func TestResolveParishFields_SlugToName_PrimaryOnlyLanguage(t *testing.T) {
	svc := model.ChurchService{ParishSlug: "helige-sergij"}
	resolveParishFields(&svc, "Helige Sergij rysk-ortodoxa församling", testSlugToParish, testNameToSlug)
	if svc.ParishLanguage == nil {
		t.Fatal("ParishLanguage should be set from uMap")
	}
	if *svc.ParishLanguage != "Kyrkoslaviska" {
		t.Errorf("ParishLanguage = %q, want %q", *svc.ParishLanguage, "Kyrkoslaviska")
	}
}

func TestResolveParishFields_ExistingParishLanguage_NotOverwritten(t *testing.T) {
	existing := "Serbiska"
	svc := model.ChurchService{ParishSlug: "helige-sergij", ParishLanguage: &existing}
	resolveParishFields(&svc, "Helige Sergij rysk-ortodoxa församling", testSlugToParish, testNameToSlug)
	if *svc.ParishLanguage != "Serbiska" {
		t.Errorf("ParishLanguage should not be overwritten, got %q", *svc.ParishLanguage)
	}
}

func TestResolveParishFields_NameToSlug(t *testing.T) {
	svc := model.ChurchService{Parish: "Heliga Anna av Novgorod"}
	unknown := resolveParishFields(&svc, "Google Calendar (Heliga Anna / St. Ignatios)", testSlugToParish, testNameToSlug)
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

func TestResolveParishFields_NameToSlug_SetsLanguage(t *testing.T) {
	svc := model.ChurchService{Parish: "Heliga Anna av Novgorod"}
	resolveParishFields(&svc, "Google Calendar (Heliga Anna / St. Ignatios)", testSlugToParish, testNameToSlug)
	if svc.ParishLanguage == nil {
		t.Fatal("ParishLanguage should be set after name→slug resolution")
	}
	if *svc.ParishLanguage != "Svenska" {
		t.Errorf("ParishLanguage = %q, want %q", *svc.ParishLanguage, "Svenska")
	}
}

func TestResolveParishFields_BothSet_NoOverwrite(t *testing.T) {
	svc := model.ChurchService{
		Parish:     "St. Georgios Cathedral",
		ParishSlug: "st-georgios",
	}
	unknown := resolveParishFields(&svc, "St. Georgios Cathedral", testSlugToParish, testNameToSlug)
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
	unknown := resolveParishFields(&svc, "Canonical Parish Name", testSlugToParish, testNameToSlug)
	if !unknown {
		t.Error("should report unknown when slug not in uMap and uMap data is available")
	}
	if svc.Parish != "Canonical Parish Name" {
		t.Errorf("Parish = %q, want scraper name as fallback", svc.Parish)
	}
}

func TestResolveParishFields_UnknownName(t *testing.T) {
	svc := model.ChurchService{Parish: "Okänd Församling"}
	unknown := resolveParishFields(&svc, "Okänd Församling", testSlugToParish, testNameToSlug)
	if svc.ParishSlug != "" {
		t.Errorf("ParishSlug = %q, want empty for unknown name", svc.ParishSlug)
	}
	if unknown {
		t.Error("name-to-slug miss should not report unknown")
	}
}

func TestResolveParishFields_BothEmpty_NoOp(t *testing.T) {
	svc := model.ChurchService{ServiceName: "Gudomlig Liturgi"}
	unknown := resolveParishFields(&svc, "Some Scraper", testSlugToParish, testNameToSlug)
	if svc.Parish != "" || svc.ParishSlug != "" {
		t.Errorf("expected no changes for empty Parish and ParishSlug")
	}
	if unknown {
		t.Error("both-empty case should not report unknown")
	}
}

func TestResolveParishFields_EmptyMaps_FallsBackToScraperName_NoAlert(t *testing.T) {
	svc := model.ChurchService{ParishSlug: "helige-sergij"}
	unknown := resolveParishFields(&svc, "Helige Sergij rysk-ortodoxa församling", map[string]umap.Parish{}, map[string]string{})
	if svc.Parish != "Helige Sergij rysk-ortodoxa församling" {
		t.Errorf("Parish = %q, want scraper name as fallback when maps are empty", svc.Parish)
	}
	if unknown {
		t.Error("should not report unknown when uMap data is unavailable (empty maps)")
	}
}

func TestResolveParishFields_EmptyMaps_NoLanguage(t *testing.T) {
	svc := model.ChurchService{ParishSlug: "helige-sergij"}
	resolveParishFields(&svc, "Helige Sergij rysk-ortodoxa församling", map[string]umap.Parish{}, map[string]string{})
	if svc.ParishLanguage != nil {
		t.Errorf("ParishLanguage should be nil when uMap is unavailable, got %q", *svc.ParishLanguage)
	}
}
