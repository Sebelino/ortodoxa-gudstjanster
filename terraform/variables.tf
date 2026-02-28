variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region for Cloud Run"
  type        = string
  default     = "europe-north1"
}

variable "service_name" {
  description = "Name of the Cloud Run service"
  type        = string
  default     = "ortodoxa-gudstjanster"
}

variable "image_tag" {
  description = "Docker image tag to deploy"
  type        = string
  default     = "latest"
}
