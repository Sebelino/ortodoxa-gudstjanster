output "service_url" {
  description = "URL of the deployed Cloud Run service"
  value       = google_cloud_run_v2_service.app.uri
}

output "artifact_registry_url" {
  description = "URL of the Artifact Registry repository"
  value       = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.repo.repository_id}"
}

output "storage_bucket" {
  description = "Name of the Cloud Storage bucket for Vision API results"
  value       = google_storage_bucket.store.name
}

output "service_account_email" {
  description = "Email of the Cloud Run service account"
  value       = google_service_account.cloudrun.email
}

output "ingest_job_name" {
  description = "Name of the Cloud Run ingestion job"
  value       = google_cloud_run_v2_job.ingest.name
}

output "scheduler_job_name" {
  description = "Name of the Cloud Scheduler job"
  value       = google_cloud_scheduler_job.ingest.name
}

output "firestore_database" {
  description = "Firestore database name"
  value       = google_firestore_database.main.name
}
