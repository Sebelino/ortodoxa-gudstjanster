package model

// ChurchService represents a single church service event.
type ChurchService struct {
	Source      string            `json:"source"`
	SourceURL   string            `json:"source_url,omitempty"`
	Date        string            `json:"date"`
	DayOfWeek   string            `json:"day_of_week"`
	ServiceName map[string]string `json:"service_name"`
	Location    *string           `json:"location"`
	Time        *string           `json:"time"`
	Occasion    *string           `json:"occasion"`
	Notes       *string           `json:"notes"`
	Language    *string           `json:"language,omitempty"`
}
