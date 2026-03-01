# Enable Cloud Scheduler API
resource "google_project_service" "cloudscheduler" {
  service            = "cloudscheduler.googleapis.com"
  disable_on_destroy = false
}

# Service account for scheduler to invoke the job
resource "google_service_account" "scheduler" {
  account_id   = "ortodoxa-scheduler-sa"
  display_name = "Scheduler Service Account for ${var.service_name}"
}

# Grant scheduler service account permission to invoke the Cloud Run Job
resource "google_cloud_run_v2_job_iam_member" "scheduler_invoker" {
  project  = var.project_id
  location = var.region
  name     = google_cloud_run_v2_job.ingest.name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.scheduler.email}"
}

# Cloud Scheduler job to trigger ingestion every 6 hours
# Note: Cloud Scheduler is not available in europe-north1, using europe-west1 instead
resource "google_cloud_scheduler_job" "ingest" {
  name        = "${var.service_name}-ingest-schedule"
  description = "Triggers the ingestion job every 6 hours"
  schedule    = "0 */6 * * *"
  time_zone   = "Europe/Stockholm"
  region      = "europe-west1"

  http_target {
    http_method = "POST"
    uri         = "https://${var.region}-run.googleapis.com/apis/run.googleapis.com/v1/namespaces/${var.project_id}/jobs/${google_cloud_run_v2_job.ingest.name}:run"

    oauth_token {
      service_account_email = google_service_account.scheduler.email
      scope                 = "https://www.googleapis.com/auth/cloud-platform"
    }
  }

  depends_on = [
    google_project_service.cloudscheduler,
    google_cloud_run_v2_job_iam_member.scheduler_invoker,
  ]
}
