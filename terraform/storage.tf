# Cloud Storage bucket for Vision API results (STORE_DIR replacement)
resource "google_storage_bucket" "store" {
  name          = "${var.project_id}-ortodoxa-store"
  location      = var.region
  force_destroy = false

  uniform_bucket_level_access = true

  versioning {
    enabled = false
  }

  lifecycle_rule {
    condition {
      age = 0 # Never delete - Vision API results are permanent
    }
    action {
      type = "Delete"
    }
  }
}

# Artifact Registry for Docker images
resource "google_artifact_registry_repository" "repo" {
  location      = var.region
  repository_id = var.service_name
  format        = "DOCKER"
  description   = "Docker images for ${var.service_name}"
}
