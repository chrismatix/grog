# terraform-provider-grog

A Terraform provider that drives [grog](https://grog.build) builds and image
pushes from infrastructure-as-code. It lets Terraform order a fast, cached grog
build — and consume the resulting image digest — within Terraform's own
dependency graph. The canonical flow:

```
artifact registry  →  grog_build  →  grog_image_push  →  cloud run (by digest)
```

## How it works

The provider embeds grog directly (it imports the public `grog/session`
package), so there is no shelling out. On `Configure` it loads the grog
workspace once and serves every resource from a single in-process session, which
bounds total build concurrency and deduplicates shared dependencies across
concurrent resource operations.

- **`grog_build`** builds a target and exposes its outputs. Container image
  outputs are published only to grog's content-addressed store (CAS), keyed by
  their local tag in an `oci_images` map. It does **not** push to a user registry.
- **`grog_image_push`** copies a built image (by manifest digest) from grog's
  CAS to an external registry, **without the local Docker daemon**, using
  go-containerregistry. Auth comes from the ambient Docker keychain
  (`~/.docker/config.json` + credential helpers such as
  `gcloud auth configure-docker`). It is convergent: a digest already present at
  the destination is not re-pushed.

## Provider configuration

| Attribute | Required | Description |
|---|---|---|
| `workspace_root` | no | Directory containing `grog.toml`. Falls back to walking up from the working directory. Set explicitly under Terragrunt. |
| `profile` | no | grog configuration profile (`grog.<profile>.toml`). |
| `skip_workspace_lock` | no | Disable grog's cross-process workspace lock. |

Cache backend and credentials are read from the workspace's `grog.toml` and
ambient cloud credentials (ADC/env) — identical to the grog CLI.

## v1 behavior and caveats

- **Always rebuild on apply.** `grog_build` re-runs on every apply and relies on
  grog's cache to make unchanged builds fast no-ops. Consequently, every plan
  shows the build's computed outputs (and anything downstream) as
  *known after apply*. A future version will add a source-closure fingerprint to
  make plans quiet and accurate. For this to be fast, use a **warm remote
  cache**.
- **Destroy is a no-op.** A built artifact cannot be "un-built"; the
  content-addressed cache entry is shared and left intact. A pushed image is not
  deleted from the registry on destroy.
- **One workspace per process.** grog configuration is process-global, so a
  single provider instance manages exactly one grog workspace. Multiple
  `provider "grog"` aliases pointing at *different* workspaces are not supported
  in v1.
- **Building a docker target still needs a Docker daemon** (grog builds the
  image via `docker build`). Only the *push* is daemon-free.
- **`grog_image_push` requires the `fs` docker backend.** With the `registry`
  backend the image already lives in a remote registry.

## Development

```sh
# Build the provider binary.
go build .

# Run unit tests (schema validation + push round-trip live in the grog module).
go test ./...
```

This module lives in the grog repository and depends on the parent module via a
local `replace grog => ../` directive. Releases ride along with the grog
`v*.*.*` tag flow — see `.github/workflows/release.yml`. Required repository
secrets for publishing: `GPG_PRIVATE_KEY` and `PASSPHRASE`, plus the GPG public
key registered in your Terraform Registry account.
