resource "grog_build" "api" {
  target = "//services/api:image"
}

# The manifest digest of an OCI image output, keyed by its local tag, is
# available for downstream resources (e.g. grog_image_push).
output "api_digest" {
  value = grog_build.api.oci_images["api:latest"].manifest_digest
}
