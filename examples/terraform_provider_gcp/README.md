# Terraform provider example — GCP

GCP variant of the [`terraform_provider`](../terraform_provider) example. Same
flow, deployed to real infrastructure:

```
google_artifact_registry_repository  →  grog_build  →  grog_image_push  →  google_cloud_run_v2_service
```

Reuses the sibling example's grog workspace (`../terraform_provider`, with its
`//app:image` docker target), so only the Terraform side differs.

## Run it

Requirements: Go, Terraform, Docker daemon, `gcloud` authenticated, and a GCP
project where you can create an Artifact Registry repository and a Cloud Run
service.

```sh
# One-time per host: configure docker auth for the region's registry.
gcloud auth configure-docker us-central1-docker.pkg.dev

# Build the provider locally (until it's published to the Registry).
( cd ../../terraform-provider-grog && go build -o /tmp/tfgrog/terraform-provider-grog . )

# Tell Terraform to use the local provider binary (skips `terraform init`).
cat > /tmp/tfgrog/dev.tfrc <<EOF
provider_installation {
  dev_overrides { "chrismatix/grog" = "/tmp/tfgrog" }
  direct {}
}
EOF
export TF_CLI_CONFIG_FILE=/tmp/tfgrog/dev.tfrc

terraform apply -var project_id=YOUR_PROJECT
```

## Notes

- Cleaning up costs money — `terraform destroy` removes Cloud Run + the
  registry repo, but the *pushed image* is not deleted from the registry
  (`grog_image_push` deliberately does not delete on destroy).
- The build still needs a local Docker daemon. Only the push is daemon-free.
- For local-only testing without a GCP project, use the sibling
  [`terraform_provider`](../terraform_provider) example.
