# Enable Firestore API
resource "google_project_service" "firestore" {
  service            = "firestore.googleapis.com"
  disable_on_destroy = false
}

# Firestore database (Native mode)
resource "google_firestore_database" "main" {
  name        = "(default)"
  location_id = var.region
  type        = "FIRESTORE_NATIVE"

  depends_on = [google_project_service.firestore]
}

# Composite index for efficient queries by source and date
resource "google_firestore_index" "services_source_date" {
  database   = google_firestore_database.main.name
  collection = "services"

  fields {
    field_path = "source"
    order      = "ASCENDING"
  }

  fields {
    field_path = "date"
    order      = "ASCENDING"
  }

  depends_on = [google_firestore_database.main]
}
