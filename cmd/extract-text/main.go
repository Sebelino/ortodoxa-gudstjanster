package main

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"regexp"
)

func main() {
	url := "https://www.ryskaortodoxakyrkan.se/gudstjänst"
	if len(os.Args) > 1 {
		url = os.Args[1]
	}

	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching URL: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading body: %v\n", err)
		os.Exit(1)
	}

	// Strip HTML tags and decode entities
	content := string(bodyBytes)
	content = regexp.MustCompile(`<[^>]*>`).ReplaceAllString(content, " ")
	content = html.UnescapeString(content)
	content = regexp.MustCompile(`\s+`).ReplaceAllString(content, " ")

	// Extract just the schedule section (from "Januari" to "bottom of page" or similar)
	schedulePattern := regexp.MustCompile(`(?i)(Januari\s.+?)(?:bottom of page|KRISTI FÖRKLARINGS|$)`)
	if match := schedulePattern.FindStringSubmatch(content); len(match) > 1 {
		content = match[1]
	}

	// Now add newlines for readability
	// Add newline before month names
	content = regexp.MustCompile(`\s+(Januari|Februari|Mars|April|Maj|Juni|Juli|Augusti|September|Oktober|November|December)\s`).ReplaceAllString(content, "\n\n$1\n")
	// Add newline before date entries (number followed by day name)
	content = regexp.MustCompile(`\s+(\d{1,2}\s+(?:Söndag|Måndag|Tisdag|Onsdag|Torsdag|Fredag|Lördag))`).ReplaceAllString(content, "\n$1")

	fmt.Println(content)
}
