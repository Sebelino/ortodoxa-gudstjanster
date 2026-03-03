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

# Grant ingestion service account read access to uploads bucket
resource "google_storage_bucket_iam_member" "ingest_uploads_access" {
  bucket = google_storage_bucket.uploads.name
  role   = "roles/storage.objectViewer"
  member = "serviceAccount:${google_service_account.ingest.email}"
}

# Grant ingestion service account read access to manual events bucket
resource "google_storage_bucket_iam_member" "ingest_manual_events_access" {
  bucket = google_storage_bucket.manual_events.name
  role   = "roles/storage.objectViewer"
  member = "serviceAccount:${google_service_account.ingest.email}"
}

# Grant ingestion service account access to read OpenAI API key secret
resource "google_secret_manager_secret_iam_member" "ingest_openai_api_key_access" {
  secret_id = google_secret_manager_secret.openai_api_key.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.ingest.email}"
}

# Grant ingestion service account access to SMTP secrets (for alerting)
resource "google_secret_manager_secret_iam_member" "ingest_smtp_host_access" {
  secret_id = google_secret_manager_secret.smtp_host.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.ingest.email}"
}

resource "google_secret_manager_secret_iam_member" "ingest_smtp_port_access" {
  secret_id = google_secret_manager_secret.smtp_port.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.ingest.email}"
}

resource "google_secret_manager_secret_iam_member" "ingest_smtp_user_access" {
  secret_id = google_secret_manager_secret.smtp_user.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.ingest.email}"
}

resource "google_secret_manager_secret_iam_member" "ingest_smtp_pass_access" {
  secret_id = google_secret_manager_secret.smtp_pass.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.ingest.email}"
}

resource "google_secret_manager_secret_iam_member" "ingest_smtp_to_access" {
  secret_id = google_secret_manager_secret.smtp_to.id
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
          name  = "GCS_UPLOAD_BUCKET"
          value = google_storage_bucket.uploads.name
        }

        env {
          name  = "GCS_MANUAL_EVENTS_BUCKET"
          value = google_storage_bucket.manual_events.name
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

        # SMTP secrets for alerting
        env {
          name = "SMTP_HOST"
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.smtp_host.secret_id
              version = "latest"
            }
          }
        }

        env {
          name = "SMTP_PORT"
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.smtp_port.secret_id
              version = "latest"
            }
          }
        }

        env {
          name = "SMTP_USER"
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.smtp_user.secret_id
              version = "latest"
            }
          }
        }

        env {
          name = "SMTP_PASS"
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.smtp_pass.secret_id
              version = "latest"
            }
          }
        }

        env {
          name = "SMTP_TO"
          value_source {
            secret_key_ref {
              secret  = google_secret_manager_secret.smtp_to.secret_id
              version = "latest"
            }
          }
        }
      }
    }
  }

  depends_on = [
    google_secret_manager_secret_iam_member.ingest_openai_api_key_access,
    google_secret_manager_secret_iam_member.ingest_smtp_host_access,
    google_secret_manager_secret_iam_member.ingest_smtp_port_access,
    google_secret_manager_secret_iam_member.ingest_smtp_user_access,
    google_secret_manager_secret_iam_member.ingest_smtp_pass_access,
    google_secret_manager_secret_iam_member.ingest_smtp_to_access,
    google_storage_bucket_iam_member.ingest_store_access,
    google_storage_bucket_iam_member.ingest_uploads_access,
    google_storage_bucket_iam_member.ingest_manual_events_access,
    google_project_iam_member.ingest_firestore_access,
    google_firestore_database.main,
  ]
}
