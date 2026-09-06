package model

// Correction represents an override applied to a scraped event during ingestion.
// It matches events by parish_slug + date + original time (and optionally
// service_name when multiple events share the same time slot), and can
// override specific fields like time, or delete the event entirely.
type Correction struct {
	ParishSlug   string `firestore:"parish_slug" json:"parish_slug"`
	Date         string `firestore:"date" json:"date"`
	OriginalTime string `firestore:"original_time" json:"original_time"`
	ServiceName  string `firestore:"service_name,omitempty" json:"service_name,omitempty"`
	Time         string `firestore:"time,omitempty" json:"time,omitempty"`
	Delete       bool   `firestore:"delete,omitempty" json:"delete,omitempty"`
	Reason       string `firestore:"reason" json:"reason"`
}
