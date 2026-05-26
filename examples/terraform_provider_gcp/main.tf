# GCP variant of the grog Terraform provider example: provisions a real
# Artifact Registry repository, builds the image with grog, pushes it (by
# digest), then deploys Cloud Run consuming the immutable pinned reference.
#
# Terraform's dependency graph orders the steps:
#   artifact_registry  ->  grog_build  ->  grog_image_push  ->  cloud_run
#
# Requires GCP credentials and a project; see README.md.

terraform {
  required_providers {
    grog = {
      source = "chrismatix/grog"
    }
    google = {
      source = "hashicorp/google"
    }
  }
}

variable "project_id" {
  type        = string
  description = "GCP project that owns the registry and the Cloud Run service."
}

variable "region" {
  type    = string
  default = "us-central1"
}

provider "google" {
  project = var.project_id
  region  = var.region
}

provider "grog" {
  # Reuse the grog workspace from the sibling local example
  # (its grog.toml + //app:image target).
  workspace_root = "${path.module}/../terraform_provider"
}

resource "google_artifact_registry_repository" "apps" {
  location      = var.region
  repository_id = "apps"
  format        = "DOCKER"
}

resource "grog_build" "app" {
  target = "//app:image"
}

resource "grog_image_push" "app" {
  source_digest = grog_build.app.docker_images["grog-tf-demo:latest"].manifest_digest
  repository    = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.apps.repository_id}/app"
  tags          = ["latest"]
}

resource "google_cloud_run_v2_service" "app" {
  name     = "grog-tf-demo"
  location = var.region
  template {
    containers {
      image = grog_image_push.app.reference # repo@sha256:…
    }
  }
}

output "image_reference" {
  value = grog_image_push.app.reference
}
