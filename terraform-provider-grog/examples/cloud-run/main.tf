# End-to-end example: provision the artifact registry, build the image with grog,
# push it, then deploy Cloud Run consuming the immutable digest. Terraform's own
# dependency graph orders these steps via the references between resources.

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

provider "google" {
  project = var.project_id
  region  = var.region
}

provider "grog" {
  # The directory containing grog.toml. Under Terragrunt set this explicitly;
  # otherwise the provider walks up from the working directory to find grog.toml.
  workspace_root = var.grog_workspace_root
}

variable "project_id" { type = string }
variable "region" { default = "us-central1" }
variable "grog_workspace_root" { type = string }

locals {
  repo_host = "${var.region}-docker.pkg.dev"
}

# 1. Create the Artifact Registry repository.
resource "google_artifact_registry_repository" "apps" {
  location      = var.region
  repository_id = "apps"
  format        = "DOCKER"
}

# 2. Build the image with grog. This only populates grog's content-addressed
#    cache (CAS); it does not push anywhere yet. Exposes the manifest digest.
resource "grog_build" "api" {
  target = "//services/api:image"
}

# 3. Push the built image to the registry created in step 1. The destination
#    repository is known only after the registry exists, so it flows in here.
resource "grog_image_push" "api" {
  source_digest = grog_build.api.docker_images["api:latest"].manifest_digest
  repository    = "${local.repo_host}/${var.project_id}/${google_artifact_registry_repository.apps.repository_id}/api"
  tags          = ["latest"]
}

# 4. Deploy Cloud Run consuming the immutable, pinned digest reference.
resource "google_cloud_run_v2_service" "api" {
  name     = "api"
  location = var.region

  template {
    containers {
      image = grog_image_push.api.reference # repo@sha256:…
    }
  }
}

output "image_reference" {
  value = grog_image_push.api.reference
}
