variable "domain" {
  description = "Custom domain for the service"
  type        = string
  default     = ""
}

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
        name  = "GCP_PROJECT_ID"
        value = var.project_id
      }

      env {
        name  = "FIRESTORE_COLLECTION"
        value = "services"
      }

      # Secrets from Secret Manager
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
    google_secret_manager_secret_iam_member.smtp_host_access,
    google_secret_manager_secret_iam_member.smtp_port_access,
    google_secret_manager_secret_iam_member.smtp_user_access,
    google_secret_manager_secret_iam_member.smtp_pass_access,
    google_secret_manager_secret_iam_member.smtp_to_access,
    google_project_iam_member.cloudrun_firestore_access,
    google_firestore_database.main,
  ]
}

# Custom domain mapping (optional)
resource "google_cloud_run_domain_mapping" "custom_domain" {
  count    = var.domain != "" ? 1 : 0
  location = var.region
  name     = var.domain

  metadata {
    namespace = var.project_id
  }

  spec {
    route_name = google_cloud_run_v2_service.app.name
  }
}

# Output DNS records needed for domain verification
output "domain_dns_records" {
  description = "DNS records to configure for custom domain"
  value       = var.domain != "" ? google_cloud_run_domain_mapping.custom_domain[0].status[0].resource_records : []
}
