package umap

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const (
	BaseURL     = "https://umap.openstreetmap.fr"
	MapID       = 1414550
	DatalayerID = "fd67393a-45e2-4348-bfa3-25ea3ad718c6"
)

type Parish struct {
	Name               string   `json:"name" firestore:"name"`
	Slug               string   `json:"slug" firestore:"slug"`
	ShortName          string   `json:"short_name" firestore:"short_name"`
	Address            string   `json:"address" firestore:"address"`
	City               string   `json:"city" firestore:"city"`
	County             string   `json:"county" firestore:"county"`
	Websites           []string `json:"websites" firestore:"websites"`
	PrimaryLanguage    string   `json:"primary_language" firestore:"primary_language"`
	SecondaryLanguages []string `json:"secondary_languages" firestore:"secondary_languages"`
	Tradition          string   `json:"tradition" firestore:"tradition"`
	Patriarchate       string   `json:"patriarchate" firestore:"patriarchate"`
	MapQuery           string   `json:"map_query" firestore:"map_query"`
	Lat                float64  `json:"lat" firestore:"lat"`
	Lng                float64  `json:"lng" firestore:"lng"`
}

type featureCollection struct {
	Features []feature `json:"features"`
}

type feature struct {
	Geometry   geometry       `json:"geometry"`
	Properties map[string]any `json:"properties"`
}

type geometry struct {
	Coordinates [2]float64 `json:"coordinates"`
}

// FetchParishes fetches parish data from the uMap datalayer.
func FetchParishes() ([]Parish, error) {
	url := fmt.Sprintf("%s/en/datalayer/%d/%s/", BaseURL, MapID, DatalayerID)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetching umap datalayer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("umap returned HTTP %d", resp.StatusCode)
	}

	var fc featureCollection
	if err := json.NewDecoder(resp.Body).Decode(&fc); err != nil {
		return nil, fmt.Errorf("decoding umap response: %w", err)
	}

	parishes := make([]Parish, 0, len(fc.Features))
	for _, f := range fc.Features {
		p := Parish{
			Name:            str(f.Properties["name"]),
			Slug:            str(f.Properties["slug"]),
			ShortName:       str(f.Properties["short_name"]),
			Address:         str(f.Properties["address"]),
			City:            str(f.Properties["city"]),
			County:          str(f.Properties["county"]),
			PrimaryLanguage: str(f.Properties["primary_language"]),
			Tradition:       str(f.Properties["tradition"]),
			Patriarchate:    str(f.Properties["patriarchate"]),
			MapQuery:        str(f.Properties["map_query"]),
			Lat:             f.Geometry.Coordinates[1],
			Lng:             f.Geometry.Coordinates[0],
		}
		if sl := str(f.Properties["secondary_languages"]); sl != "" {
			for _, lang := range strings.Split(sl, ",") {
				lang = strings.TrimSpace(lang)
				if lang != "" {
					p.SecondaryLanguages = append(p.SecondaryLanguages, lang)
				}
			}
		}
		if w := str(f.Properties["website"]); w != "" {
			for _, url := range strings.Split(w, ",") {
				url = strings.TrimSpace(url)
				if url != "" {
					p.Websites = append(p.Websites, url)
				}
			}
		}
		if p.Slug == "" {
			continue // skip markers without a slug
		}
		parishes = append(parishes, p)
	}
	return parishes, nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
