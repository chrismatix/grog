terraform {
  required_providers {
    grog = {
      source = "chrismatix/grog"
    }
  }
}

provider "grog" {
  # This directory is the grog workspace (it contains grog.toml).
  workspace_root = path.module
}

# Build the docker image target with grog. This populates grog's
# content-addressed store and exposes the manifest digest; it does not push.
resource "grog_build" "app" {
  target = "//app:image"
}

# Push the built image (by digest) to the local registry started by run.sh.
# Auth uses the ambient Docker keychain; localhost registries are plain HTTP.
resource "grog_image_push" "app" {
  source_digest = grog_build.app.docker_images["grog-tf-demo:latest"].manifest_digest
  repository    = "localhost:5001/grog-tf-demo"
  tags          = ["latest"]
}

output "change_hash" {
  description = "grog's cache key for the build."
  value       = grog_build.app.change_hash
}

output "image_reference" {
  description = "Immutable pinned reference to feed a runtime (Cloud Run, k8s, …)."
  value       = grog_image_push.app.reference
}
