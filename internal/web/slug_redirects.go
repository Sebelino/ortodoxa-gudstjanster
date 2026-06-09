package web

// slugRedirects maps old parish slugs to their canonical replacements.
// Entries here cause /parish/{old} to 301-redirect to /parish/{new}.
var slugRedirects = map[string]string{
	"aposteln-andreas":        "st-andreas",
	"aposteln-kleopas":        "aposteln-cleopas",
	"demetrios-nestor":        "de-helige-demetrios-nestor",
	"finska-ortodoxa":         "helige-nikolai",
	"gudsmoder-tempelgang":    "gudafoderskans-tempelgang",
	"heliga-treenigheten-gbg": "heliga-treenigheten-goteborg",
	"konstantin-helena":       "de-heliga-konstantin-helena",
	"kristi-forklarings":      "kristi-forklaring",
	"nikolaus-eremitage":      "helige-nikolai-eremitage",
	"st-paulus":               "apostolos-paulos",
	"rumanien-boden":          "rumanska-boden",
	"rumanien-boras":          "rumanska-boras",
	"rumanien-eskilstuna":     "rumanska-eskilstuna",
	"rumanien-gavle":          "rumanska-gavle",
	"rumanien-goteborg":       "rumanska-goteborg",
	"rumanien-helsingborg":    "rumanska-helsingborg",
	"rumanien-huddinge":       "rumanska-huddinge",
	"rumanien-jonkoping":      "rumanska-jonkoping",
	"rumanien-kalmar":         "rumanska-kalmar",
	"rumanien-lund":           "rumanska-lund",
	"rumanien-malmo":          "rumanska-malmo",
	"rumanien-solvesborg":     "rumanska-solvesborg",
	"rumanien-trollhattan":    "rumanska-trollhattan",
	"rumanien-tungelsta":      "rumanska-tungelsta",
	"rumanien-uppsala":        "rumanska-uppsala",
	"rumanien-vasteras":       "rumanska-vasteras",
	"rumanien-vaxjo":          "rumanska-vaxjo",
}
