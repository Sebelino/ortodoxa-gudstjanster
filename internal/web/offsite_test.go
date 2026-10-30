package web

import (
	"strings"
	"testing"

	"ortodoxa-gudstjanster/internal/model"
	"ortodoxa-gudstjanster/internal/umap"
)

func TestStreetKey(t *testing.T) {
	tests := []struct{ address, want string }{
		{"Birger Jarlsgatan 92, Stockholm", "birger jarlsgatan 92"},
		{"Nynäsvägen 3C, 136 47 Haninge", "nynäsvägen 3c"},
		{"Matteus Kapelle, Sannaplan 3, Göteborg", "sannaplan 3"},
		{"Flemingsbergs kyrka, Diagnosvägen 14-16, Huddinge", "diagnosvägen 14"},
		{"Borgs kyrka, 605 97 Norrköping", ""},
		{"Trollhättan", ""},
		{"Kontakta församlingen för information", ""},
	}
	for _, tt := range tests {
		if got := streetKey(tt.address); got != tt.want {
			t.Errorf("streetKey(%q) = %q, want %q", tt.address, got, tt.want)
		}
	}
}

func TestOffsiteVenue(t *testing.T) {
	original := make([]umap.Parish, 0, len(parishes))
	for _, p := range parishes {
		original = append(original, umap.Parish{Slug: p.Slug, Name: p.Name, ShortName: p.ShortName, Address: p.Address})
	}
	t.Cleanup(func() { SetParishes(original) })
	SetParishes([]umap.Parish{
		{Slug: "st-georgios", Name: "St. Georgios Cathedral", ShortName: "St. Georgios", Address: "Birger Jarlsgatan 92, Stockholm"},
		{Slug: "kristi-forklarings", Name: "Kristi Förklarings", ShortName: "Kristi Förklaring", Address: "Birger Jarlsgatan 98, Stockholm"},
		{Slug: "nikolai", Name: "Helige Nikolai", ShortName: "Helige Nikolai", Address: "Bellmansgatan 13, Stockholm"},
		{Slug: "sava", Name: "Sankt Sava", ShortName: "Sankt Sava", Address: "Bägerstavägen 68, Enskede"},
		{Slug: "anna", Name: "Heliga Anna", ShortName: "Heliga Anna", Address: "Kyrkvägen 27, Stocksund"},
		{Slug: "giorgis", Name: "Helige Giorgis", ShortName: "Helige Giorgis", Address: "Kyrkvägen 27, Stocksund"},
		{Slug: "norrkoping", Name: "Norrköping", ShortName: "Norrköping", Address: "Borgs kyrka, 605 97 Norrköping"},
	})

	tests := []struct {
		name     string
		slug     string
		parish   string
		location *string
		want     string
	}{
		{"home, same format", "st-georgios", "", ptr("Birger Jarlsgatan 92, 114 20 Stockholm"), ""},
		{"home, venue prefix", "nikolai", "", ptr("Helige Nikolai ortodoxa kyrka, Bellmansgatan 13, 118 47 Stockholm, Sweden"), ""},
		{"home, leading space", "sava", "", ptr(" Bägerstavägen 68, Enskede"), ""},
		{"known parish by name", "", "St. Georgios Cathedral", ptr("Bellmansgatan 13, 118 47 Stockholm"), "Helige Nikolai"},
		{"neighboring house number", "st-georgios", "", ptr("Birger Jarlsgatan 98, 114 20 Stockholm"), "Kristi Förklaring"},
		{"unknown venue", "nikolai", "", ptr("Finska kyrkan, Slottsbacken 2 / Suomalainen kirkko, Slottsbacken 2"), "Finska kyrkan"},
		{"shared address", "sava", "", ptr("Kyrkvägen 27, 182 74 Stocksund"), "Kyrkvägen 27"},
		{"no location", "sava", "", nil, ""},
		{"blank location", "sava", "", ptr("  "), ""},
		{"parish without street", "norrkoping", "", ptr("Bellmansgatan 13"), ""},
		{"unknown parish", "", "Okänd", ptr("Bellmansgatan 13"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := model.ChurchService{ParishSlug: tt.slug, Parish: tt.parish, Location: tt.location}
			if got := offsiteVenue(s); got != tt.want {
				t.Errorf("offsiteVenue() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerateICSMarksOffsiteVenue(t *testing.T) {
	ics := generateICS([]model.ChurchService{{
		Parish: "St. Georgios Cathedral", Date: "2026-10-31", ServiceName: "Gudomlig Liturgi", OffsiteVenue: "Helige Nikolai",
	}})
	if !strings.Contains(ics, "SUMMARY:Gudomlig Liturgi (obs: Helige Nikolai)") {
		t.Errorf("summary is missing the venue:\n%s", ics)
	}
}
