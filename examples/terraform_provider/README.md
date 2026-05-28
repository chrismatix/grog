# Terraform provider example

A self-contained, locally-runnable demo of the grog Terraform provider. This
directory **is** a grog workspace: it has a `grog.toml` and an `//app:image`
docker target. Terraform builds that target with grog, then pushes the resulting
image (by digest) to a local registry.

```
//app:image  ──grog_build──▶  CAS  ──grog_image_push──▶  localhost:5001/grog-tf-demo
                                                          │
                                              reference = repo@sha256:…  (output)
```

## Run it (one command)

Requirements: Go, Terraform, and a running Docker daemon.

```sh
./run.sh -auto-approve
```

`run.sh` builds the provider from source, starts a local Docker registry on
`localhost:5001`, writes a Terraform `dev_overrides` config (so no
`terraform init` is needed), and applies. Then verify the push:

```sh
curl -s http://localhost:5001/v2/grog-tf-demo/tags/list
# {"name":"grog-tf-demo","tags":["latest"]}
```

Tear everything down (including the local registry):

```sh
./run.sh destroy
```

## What to look at

- `app/BUILD.yaml` + `app/Dockerfile` — the grog docker target.
- `main.tf` — the provider, `grog_build`, and `grog_image_push` wired together.
  The Cloud Run service would consume `grog_image_push.app.reference`.
- [`../terraform_provider_gcp/`](../terraform_provider_gcp) — the same flow
  against real GCP Artifact Registry + Cloud Run.

## Notes

- The build re-runs on every `apply`; grog's cache makes unchanged builds fast
  no-ops, but the plan shows the digest as *known after apply* (a v1 limitation).
- Building the docker target needs a Docker daemon. The **push** itself is
  daemon-free (it streams from grog's CAS via go-containerregistry).
- Side effect of `docker build`: the image is also tagged locally as
  `grog-tf-demo:latest`, so you can `docker run grog-tf-demo:latest` after the
  apply without pulling from the registry.
- `localhost` registries are plain HTTP, which go-containerregistry handles
  automatically. For a real registry, authenticate with the Docker keychain
  (e.g. `gcloud auth configure-docker`).
