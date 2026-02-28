# Cloud Run service
resource "google_cloud_run_v2_service" "app" {
  name     = var.service_name
  location = var.region
  ingress  = "INGRESS_TRAFFIC_ALL"

  template {
    service_account = google_service_account.cloudrun.email

    scaling {
      min_instance_count = 0
      max_instance_count = 2
    }

    containers {
      image = "${var.region}-docker.pkg.dev/${var.project_id}/${var.service_name}/${var.service_name}:${var.image_tag}"

      ports {
        container_port = 8080
      }

      resources {
        limits = {
          cpu    = "1"
          memory = "512Mi"
        }
      }

      # Environment variables
      env {
        name  = "CACHE_DIR"
        value = "/tmp/cache"
      }

      env {
        name  = "GCS_BUCKET"
        value = google_storage_bucket.store.name
      }

      # Secrets from Secret Manager
      env {
        name = "OPENAI_API_KEY"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.openai_api_key.secret_id
            version = "latest"
          }
        }
      }

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

  depends_on = [
    google_secret_manager_secret_iam_member.openai_api_key_access,
    google_secret_manager_secret_iam_member.smtp_host_access,
    google_secret_manager_secret_iam_member.smtp_port_access,
    google_secret_manager_secret_iam_member.smtp_user_access,
    google_secret_manager_secret_iam_member.smtp_pass_access,
    google_secret_manager_secret_iam_member.smtp_to_access,
    google_storage_bucket_iam_member.store_access,
  ]
}
