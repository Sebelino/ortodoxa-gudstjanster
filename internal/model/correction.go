package model

// Correction represents an override applied to a scraped event during ingestion.
// It matches events by parish_slug + date + original time, and can override
// specific fields like time.
type Correction struct {
	ParishSlug   string `firestore:"parish_slug" json:"parish_slug"`
	Date         string `firestore:"date" json:"date"`
	OriginalTime string `firestore:"original_time" json:"original_time"`
	Time         string `firestore:"time,omitempty" json:"time,omitempty"`
	Reason       string `firestore:"reason" json:"reason"`
}
