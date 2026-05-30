//go:build ignore

// Script to delete a marker from the "Ortodoxi i Sverige" uMap.
//
// Usage:
//
//	go run scripts/umap-delete-marker.go \
//	  -name "St. Georgios Cathedral" \
//	  -session-id "abc123"
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
)

const (
	mapID       = 1414550
	datalayerID = "fd67393a-45e2-4348-bfa3-25ea3ad718c6"
	baseURL     = "https://umap.openstreetmap.fr"
)

type feature struct {
	Type       string         `json:"type"`
	Geometry   json.RawMessage `json:"geometry"`
	Properties map[string]any `json:"properties"`
	ID         string         `json:"id"`
}

type featureCollection struct {
	Type       string         `json:"type"`
	Features   []feature      `json:"features"`
	Properties map[string]any `json:"properties"`
	ID         string         `json:"id"`
	Rank       int            `json:"rank"`
}

type mapGeoJSON struct {
	Properties struct {
		Datalayers []struct {
			ID               string `json:"id"`
			ReferenceVersion string `json:"referenceVersion"`
		} `json:"datalayers"`
	} `json:"properties"`
}

func main() {
	name := flag.String("name", "", "Name of the marker to delete (required)")
	sessionID := flag.String("session-id", "", "uMap session cookie (required)")
	csrfToken := flag.String("csrf-token", "", "CSRF token (auto-fetched if omitted)")
	flag.Parse()

	if *name == "" || *sessionID == "" {
		fmt.Fprintln(os.Stderr, "Error: -name and -session-id are required.")
		flag.Usage()
		os.Exit(1)
	}

	cookies := []*http.Cookie{
		{Name: "sessionid", Value: *sessionID},
	}

	if *csrfToken == "" {
		token, err := fetchCSRFToken(*sessionID)
		if err != nil {
			log.Fatalf("Fetching CSRF token: %v", err)
		}
		*csrfToken = token
	}
	cookies = append(cookies, &http.Cookie{Name: "csrftoken", Value: *csrfToken})

	refVersion, err := fetchReferenceVersion()
	if err != nil {
		log.Fatalf("Fetching reference version: %v", err)
	}

	current, err := fetchDatalayer()
	if err != nil {
		log.Fatalf("Fetching datalayer: %v", err)
	}

	// Find and remove the marker
	found := false
	filtered := make([]feature, 0, len(current.Features))
	for _, f := range current.Features {
		if n, _ := f.Properties["name"].(string); n == *name {
			found = true
			continue
		}
		filtered = append(filtered, f)
	}

	if !found {
		fmt.Fprintf(os.Stderr, "Error: no marker with name %q found.\nExisting markers:\n", *name)
		for _, f := range current.Features {
			n, _ := f.Properties["name"].(string)
			fmt.Fprintf(os.Stderr, "  - %s (id: %s)\n", n, f.ID)
		}
		os.Exit(1)
	}

	current.Features = filtered

	settingsJSON, err := json.Marshal(current.Properties)
	if err != nil {
		log.Fatalf("Marshaling settings: %v", err)
	}
	geojsonJSON, err := json.Marshal(current)
	if err != nil {
		log.Fatalf("Marshaling geojson: %v", err)
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	w.WriteField("name", "Layer 1")
	w.WriteField("parent", "")
	w.WriteField("display_on_load", "true")
	w.WriteField("rank", "0")
	w.WriteField("settings", string(settingsJSON))

	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", `form-data; name="geojson"; filename="blob"`)
	partHeader.Set("Content-Type", "application/json")
	part, err := w.CreatePart(partHeader)
	if err != nil {
		log.Fatalf("Creating form part: %v", err)
	}
	part.Write(geojsonJSON)
	w.Close()

	url := fmt.Sprintf("%s/en/map/%d/datalayer/update/%s/", baseURL, mapID, datalayerID)
	req, err := http.NewRequest("POST", url, &body)
	if err != nil {
		log.Fatalf("Creating request: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Origin", baseURL)
	req.Header.Set("Referer", fmt.Sprintf("%s/en/map/ortodoxi-i-sverige_%d", baseURL, mapID))
	req.Header.Set("X-CSRFToken", *csrfToken)
	req.Header.Set("X-Datalayer-Reference", refVersion)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("Sending request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		log.Fatalf("HTTP %d: %s", resp.StatusCode, respBody)
	}

	fmt.Printf("Marker %q deleted successfully.\n", *name)
}

func fetchCSRFToken(sessionID string) (string, error) {
	req, err := http.NewRequest("GET", baseURL+"/en/", nil)
	if err != nil {
		return "", err
	}
	req.AddCookie(&http.Cookie{Name: "sessionid", Value: sessionID})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	for _, c := range resp.Cookies() {
		if c.Name == "csrftoken" {
			return c.Value, nil
		}
	}
	return "", fmt.Errorf("csrftoken cookie not found in response")
}

func fetchReferenceVersion() (string, error) {
	resp, err := http.Get(fmt.Sprintf("%s/en/map/%d/geojson/", baseURL, mapID))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var data mapGeoJSON
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	for _, dl := range data.Properties.Datalayers {
		if dl.ID == datalayerID {
			return dl.ReferenceVersion, nil
		}
	}
	return "", fmt.Errorf("datalayer %s not found", datalayerID)
}

func fetchDatalayer() (*featureCollection, error) {
	resp, err := http.Get(fmt.Sprintf("%s/en/datalayer/%d/%s/", baseURL, mapID, datalayerID))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var fc featureCollection
	if err := json.NewDecoder(resp.Body).Decode(&fc); err != nil {
		return nil, err
	}
	return &fc, nil
}
