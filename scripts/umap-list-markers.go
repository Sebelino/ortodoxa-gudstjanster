// Script to list all markers on the "Ortodoxi i Sverige" uMap.
//
// Usage:
//
//	go run scripts/umap-list-markers.go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
)

const (
	mapID       = 1414550
	datalayerID = "fd67393a-45e2-4348-bfa3-25ea3ad718c6"
	baseURL     = "https://umap.openstreetmap.fr"
)

type feature struct {
	Type     string `json:"type"`
	Geometry struct {
		Coordinates [2]float64 `json:"coordinates"`
	} `json:"geometry"`
	Properties map[string]any `json:"properties"`
	ID         string         `json:"id"`
}

type featureCollection struct {
	Features []feature `json:"features"`
}

func main() {
	url := fmt.Sprintf("%s/en/datalayer/%d/%s/", baseURL, mapID, datalayerID)
	resp, err := http.Get(url)
	if err != nil {
		log.Fatalf("Fetching datalayer: %v", err)
	}
	defer resp.Body.Close()

	var fc featureCollection
	if err := json.NewDecoder(resp.Body).Decode(&fc); err != nil {
		log.Fatalf("Decoding response: %v", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(fc.Features); err != nil {
		log.Fatalf("Encoding JSON: %v", err)
	}
}
