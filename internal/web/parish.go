package web

import (
	"net/url"
	"strings"

	"ortodoxa-gudstjanster/internal/umap"
)

// ParishInfo holds information about an Orthodox parish.
type ParishInfo struct {
	Slug               string
	Name               string
	ShortName          string
	Address            string
	City               string
	County             string // short county name used in filter params, e.g. "Stockholm"
	Website            string
	PrimaryLanguage    string   // main liturgical language; used for language filtering of unlabelled events
	SecondaryLanguages []string // additional languages used at this parish
	Tradition          string
	Patriarchate       string
	MapQuery           string
	Lat                float64
	Lng                float64
}

var parishes []ParishInfo
var parishBySlug map[string]ParishInfo

// SetParishes replaces the in-memory parish list. Must be called before serving requests.
func SetParishes(umapParishes []umap.Parish) {
	parishes = make([]ParishInfo, len(umapParishes))
	for i, p := range umapParishes {
		parishes[i] = ParishInfo{
			Slug:               p.Slug,
			Name:               p.Name,
			ShortName:          p.ShortName,
			Address:            p.Address,
			City:               p.City,
			County:             shortCounty(p.County),
			Website:            p.Website,
			PrimaryLanguage:    p.PrimaryLanguage,
			SecondaryLanguages: p.SecondaryLanguages,
			Tradition:          p.Tradition,
			Patriarchate:       p.Patriarchate,
			MapQuery:           buildMapQuery(p.Name, p.Address),
			Lat:                p.Lat,
			Lng:                p.Lng,
		}
	}

	parishBySlug = make(map[string]ParishInfo, len(parishes))
	for _, p := range parishes {
		parishBySlug[p.Slug] = p
	}
}

// shortCounty converts "Stockholms län" → "Stockholm", "Västra Götalands län" → "Västra Götaland".
func shortCounty(county string) string {
	county = strings.TrimSuffix(county, "s län")
	county = strings.TrimSuffix(county, " län")
	return county
}

// buildMapQuery creates a Google Maps search query from the parish name and address.
func buildMapQuery(name, address string) string {
	return strings.ReplaceAll(url.PathEscape(name+" "+address), "%20", "+")
}

var countyNames = map[string]string{
	"Stockholm":       "Stockholms län",
	"Västra Götaland": "Västra Götalands län",
}

func countyDisplayName(county string) string {
	if name, ok := countyNames[county]; ok {
		return name
	}
	return county
}
