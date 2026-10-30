package web

import (
	"regexp"
	"strings"
	"unicode"

	"ortodoxa-gudstjanster/internal/model"
)

// streetRe matches an address segment that starts with a street and number,
// e.g. "Birger Jarlsgatan 92" or "Nynäsvägen 3C". It deliberately can't match
// postal segments ("605 97 Norrköping") or bare venue names ("Trollhättan").
var streetRe = regexp.MustCompile(`^\p{L}[\p{L}. -]*\s\d+\p{L}?`)

// streetKey returns the lowercased street and number from an address, or "" if
// it has none. Parish addresses may lead with a venue name ("Matteus Kapelle,
// Sannaplan 3, Göteborg"), so every comma-separated segment is considered.
func streetKey(address string) string {
	for _, segment := range strings.Split(address, ",") {
		if street := streetRe.FindString(normalizeAddress(segment)); street != "" {
			return street
		}
	}
	return ""
}

func normalizeAddress(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// hasStreet reports whether location mentions the street key as whole words, so
// that "birger jarlsgatan 9" doesn't match "Birger Jarlsgatan 92". Scrapers
// format the same address in many ways, which rules out comparing them whole.
func hasStreet(location, key string) bool {
	location = normalizeAddress(location)
	for offset := 0; ; {
		i := strings.Index(location[offset:], key)
		if i < 0 {
			return false
		}
		start, end := offset+i, offset+i+len(key)
		before := []rune(location[:start])
		after := []rune(location[end:])
		if (len(before) == 0 || !isWordRune(before[len(before)-1])) &&
			(len(after) == 0 || !isWordRune(after[0])) {
			return true
		}
		offset = end
	}
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// offsiteVenue returns a short name for where a service is held if that differs
// from the parish's own address, and "" otherwise. Services with no location,
// and parishes without a comparable address, are never treated as off-site.
func offsiteVenue(s model.ChurchService) string {
	if s.Location == nil || strings.TrimSpace(*s.Location) == "" {
		return ""
	}
	parish, ok := parishBySlug[s.ParishSlug]
	if !ok {
		parish, ok = parishByName[s.Parish]
	}
	if !ok || parish.street == "" || hasStreet(*s.Location, parish.street) {
		return ""
	}

	// Prefer the name of a known parish at that address. An address shared by
	// several parishes can't be attributed to one of them.
	var hosts []ParishInfo
	for _, p := range parishes {
		if p.street != "" && hasStreet(*s.Location, p.street) {
			hosts = append(hosts, p)
		}
	}
	if len(hosts) == 1 {
		return hosts[0].ShortName
	}
	venue, _, _ := strings.Cut(*s.Location, ",")
	return strings.TrimSpace(venue)
}
