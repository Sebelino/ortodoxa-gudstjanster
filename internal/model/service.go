package model

import "time"

// ChurchService represents a single church service event.
type ChurchService struct {
	ID          string     `json:"id,omitempty"`
	Parish      string     `json:"parish"`
	Source      string     `json:"source"`
	SourceURL   string     `json:"source_url,omitempty"`
	Date        string     `json:"date"`
	DayOfWeek   string     `json:"day_of_week"`
	ServiceName string     `json:"service_name"`
	Title       string     `json:"title,omitempty"`
	Location    *string    `json:"location"`
	Time        *string    `json:"time"`
	StartTime   *time.Time `json:"start_time,omitempty"`
	EndTime     *time.Time `json:"end_time,omitempty"`
	Occasion    *string    `json:"occasion"`
	Notes       *string    `json:"notes"`
	Language       *string    `json:"language,omitempty"`
	ParishLanguage *string    `json:"parish_language,omitempty"`
	EventLanguage  *string    `json:"event_language,omitempty"`
}
