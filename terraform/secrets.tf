# Secret Manager secrets for sensitive configuration

resource "google_secret_manager_secret" "openai_api_key" {
  secret_id = "${var.service_name}-openai-api-key"

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret" "smtp_host" {
  secret_id = "${var.service_name}-smtp-host"

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret" "smtp_port" {
  secret_id = "${var.service_name}-smtp-port"

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret" "smtp_user" {
  secret_id = "${var.service_name}-smtp-user"

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret" "smtp_pass" {
  secret_id = "${var.service_name}-smtp-pass"

  replication {
    auto {}
  }
}

resource "google_secret_manager_secret" "smtp_to" {
  secret_id = "${var.service_name}-smtp-to"

  replication {
    auto {}
  }
}

# Grant Cloud Run service account access to read secrets
resource "google_secret_manager_secret_iam_member" "openai_api_key_access" {
  secret_id = google_secret_manager_secret.openai_api_key.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.cloudrun.email}"
}

resource "google_secret_manager_secret_iam_member" "smtp_host_access" {
  secret_id = google_secret_manager_secret.smtp_host.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.cloudrun.email}"
}

resource "google_secret_manager_secret_iam_member" "smtp_port_access" {
  secret_id = google_secret_manager_secret.smtp_port.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.cloudrun.email}"
}

resource "google_secret_manager_secret_iam_member" "smtp_user_access" {
  secret_id = google_secret_manager_secret.smtp_user.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.cloudrun.email}"
}

resource "google_secret_manager_secret_iam_member" "smtp_pass_access" {
  secret_id = google_secret_manager_secret.smtp_pass.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.cloudrun.email}"
}

resource "google_secret_manager_secret_iam_member" "smtp_to_access" {
  secret_id = google_secret_manager_secret.smtp_to.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.cloudrun.email}"
}
