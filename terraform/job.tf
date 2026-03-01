# Service account for ingestion job
resource "google_service_account" "ingest" {
  account_id   = "ortodoxa-ingest-sa"
  display_name = "Ingestion Job Service Account for ${var.service_name}"
}

# Grant ingestion service account access to Cloud Storage bucket
resource "google_storage_bucket_iam_member" "ingest_store_access" {
  bucket = google_storage_bucket.store.name
  role   = "roles/storage.objectUser"
  member = "serviceAccount:${google_service_account.ingest.email}"
}

# Grant ingestion service account access to Firestore
resource "google_project_iam_member" "ingest_firestore_access" {
  project = var.project_id
  role    = "roles/datastore.user"
  member  = "serviceAccount:${google_service_account.ingest.email}"
}

# Grant ingestion service account access to read OpenAI API key secret
resource "google_secret_manager_secret_iam_member" "ingest_openai_api_key_access" {
  secret_id = google_secret_manager_secret.openai_api_key.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.ingest.email}"
}

# Cloud Run Job for ingestion
resource "google_cloud_run_v2_job" "ingest" {
  name     = "${var.service_name}-ingest"
  location = var.region

  template {
    template {
      service_account = google_service_account.ingest.email
      timeout         = "600s"

      containers {
        image   = "${var.region}-docker.pkg.dev/${var.project_id}/${var.service_name}/${var.service_name}:${var.image_tag}"
        command = ["./ingest"]

        resources {
          limits = {
            cpu    = "2"
            memory = "1Gi"
          }
        }

        env {
          name  = "GCP_PROJECT_ID"
          value = var.project_id
        }

        env {
          name  = "FIRESTORE_COLLECTION"
          value = "services"
        }

        env {
          name  = "GCS_BUCKET"
          value = google_storage_bucket.store.name
        }

        env {
          name = "OPENAI_API_KEY"
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.openai_api_key.secret_id
              version = "latest"
            }
          }
        }
      }
    }
  }

  depends_on = [
    google_secret_manager_secret_iam_member.ingest_openai_api_key_access,
    google_storage_bucket_iam_member.ingest_store_access,
    google_project_iam_member.ingest_firestore_access,
    google_firestore_database.main,
  ]
}
