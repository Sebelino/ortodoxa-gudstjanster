terraform {
  required_version = ">= 1.0"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
  }

  # Uncomment and configure for remote state
  # backend "gcs" {
  #   bucket = "your-terraform-state-bucket"
  #   prefix = "ortodoxa-gudstjanster"
  # }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

data "google_project" "project" {}
