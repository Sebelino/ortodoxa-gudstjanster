# Service account for Cloud Run
resource "google_service_account" "cloudrun" {
  account_id   = "${var.service_name}-sa"
  display_name = "Cloud Run Service Account for ${var.service_name}"
}

# Grant service account access to Cloud Storage bucket
resource "google_storage_bucket_iam_member" "store_access" {
  bucket = google_storage_bucket.store.name
  role   = "roles/storage.objectUser"
  member = "serviceAccount:${google_service_account.cloudrun.email}"
}

# Allow unauthenticated access to Cloud Run service
resource "google_cloud_run_v2_service_iam_member" "public_access" {
  location = google_cloud_run_v2_service.app.location
  project  = google_cloud_run_v2_service.app.project
  name     = google_cloud_run_v2_service.app.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}
