resource "grog_image_push" "api" {
  source_digest = grog_build.api.oci_images["api:latest"].manifest_digest
  repository    = "us-docker.pkg.dev/my-project/apps/api"
  tags          = ["v1", "latest"]
}

# Feed the immutable pinned reference (repo@sha256:…) to your runtime.
output "api_reference" {
  value = grog_image_push.api.reference
}
