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

# Composite index for counting future services per scraper
resource "google_firestore_index" "services_scraper_date" {
  database   = google_firestore_database.main.name
  collection = "services"

  fields {
    field_path = "scraper_name"
    order      = "ASCENDING"
  }

  fields {
    field_path = "date"
    order      = "ASCENDING"
  }

  depends_on = [google_firestore_database.main]
}

# Composite index for finding a scraper's latest future service date
# (Firestore doesn't reuse an ascending composite index for a descending
# order-by, so this needs its own index rather than reusing the one above).
resource "google_firestore_index" "services_scraper_date_desc" {
  database   = google_firestore_database.main.name
  collection = "services"

  fields {
    field_path = "scraper_name"
    order      = "ASCENDING"
  }

  fields {
    field_path = "date"
    order      = "DESCENDING"
  }

  depends_on = [google_firestore_database.main]
}