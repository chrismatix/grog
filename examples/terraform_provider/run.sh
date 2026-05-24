#!/usr/bin/env bash
# One-command local run of the grog Terraform provider example:
#   1. builds the provider from source into a dev_overrides dir,
#   2. starts a local Docker registry (localhost:5001),
#   3. writes a Terraform CLI config using dev_overrides (so no `terraform init`),
#   4. runs `terraform apply`.
#
# Requirements: Go, Terraform, and a running Docker daemon.
# Usage: ./run.sh            # apply (prompts for confirmation)
#        ./run.sh -auto-approve
#        ./run.sh destroy    # tear down (also stops the local registry)
set -euo pipefail

EXAMPLE_DIR="$(cd "$(dirname "$0")" && pwd)"
PROVIDER_DIR="$EXAMPLE_DIR/../../terraform-provider-grog"
BIN_DIR="$EXAMPLE_DIR/.bin"
REGISTRY_NAME="grog-registry"
REGISTRY_PORT="5001"

if [[ "${1:-}" == "destroy" ]]; then
  export TF_CLI_CONFIG_FILE="$EXAMPLE_DIR/dev.tfrc"
  terraform -chdir="$EXAMPLE_DIR" destroy -auto-approve || true
  docker rm -f "$REGISTRY_NAME" >/dev/null 2>&1 || true
  echo "Torn down."
  exit 0
fi

echo "==> Building the provider"
mkdir -p "$BIN_DIR"
(cd "$PROVIDER_DIR" && go build -o "$BIN_DIR/terraform-provider-grog" .)

echo "==> Ensuring local registry on :$REGISTRY_PORT"
if ! docker ps --format '{{.Names}}' | grep -q "^${REGISTRY_NAME}$"; then
  docker run -d --rm -p "${REGISTRY_PORT}:5000" --name "$REGISTRY_NAME" registry:2 >/dev/null
fi

echo "==> Writing dev_overrides Terraform config"
cat > "$EXAMPLE_DIR/dev.tfrc" <<EOF
provider_installation {
  dev_overrides {
    "chrismatix/grog" = "$BIN_DIR"
  }
  direct {}
}
EOF

echo "==> terraform apply (dev_overrides => no init required)"
export TF_CLI_CONFIG_FILE="$EXAMPLE_DIR/dev.tfrc"
if [[ $# -eq 0 ]]; then
  terraform -chdir="$EXAMPLE_DIR" apply
else
  terraform -chdir="$EXAMPLE_DIR" apply "$@"
fi

echo
echo "==> Done. Verify the pushed image:"
echo "    curl -s http://localhost:${REGISTRY_PORT}/v2/grog-tf-demo/tags/list"
