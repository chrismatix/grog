#!/bin/bash

set -euo pipefail

# Store the repository root path (assuming this script is run from the repo root)
REPO_ROOT=$(pwd)
PACKAGE_PATH="$1"
PLATFORM="${2:-}"

# Check if the argument (local package path) is provided and if the path exists
if [ -z "$PACKAGE_PATH" ] || [ ! -d "$PACKAGE_PATH" ]; then
  echo "Usage: $0 <local_package_path> [platform]"
  echo "Error: Path '$PACKAGE_PATH' does not exist or is not a directory."
  exit 1
fi

if [ -n "$PLATFORM" ]; then
  echo "Building pex for $PACKAGE_PATH with docker using platform $PLATFORM"
else
  echo "Building pex for $PACKAGE_PATH with docker"
fi

if [ "${REBUILD_PEX_BUILDER:-}" == "true" ]; then
  echo "Rebuilding the pex-builder..."
  # Build the pex-builder with optional platform argument
  if [ -n "$PLATFORM" ]; then
    earthly --platform "$PLATFORM" +pex-builder
  else
    earthly +pex-builder
  fi
else
  echo "Skipping pex-builder rebuild as REBUILD_PEX_BUILDER is not set to 'true'."
fi

DIST_DIR="$PACKAGE_PATH"/dist

# If we don't clear the target bin pex will keep
# adding to the old archive!
rm -r "$DIST_DIR"
echo "Cleared dist directory $DIST_DIR"

echo "Building the pex file for $1"

if [ -d "$PACKAGE_PATH" ]; then
  cd "$PACKAGE_PATH" || { echo "Failed to cd into $PACKAGE_PATH"; exit 1; }
else
  echo "Directory $PACKAGE_PATH does not exist."
  exit 1
fi


# Generate the package specific requirements txt
uv pip compile pyproject.toml --universal -o dist/requirements.txt --quiet

# Replace the absolute host path with the container path
# Works on both macOS and Linux
if [[ "$OSTYPE" == "darwin"* ]]; then
    # macOS sed requires empty string after -i
    sed -i '' "s|file://$REPO_ROOT|file:///build|g" dist/requirements.txt
else
    # Linux sed
    sed -i "s|file://$REPO_ROOT|file:///build|g" dist/requirements.txt
fi

echo "Compiled $DIST_DIR/requirements.txt"

# go back to the root to build the pex
cd "$REPO_ROOT" || { echo "Failed to cd into $REPO_ROOT"; exit 1; }

# Build the pex with optional platform argument
if [ -n "$PLATFORM" ]; then
  docker run --rm -it --platform "$PLATFORM" -v "$(pwd):/build" -w /build/"$PACKAGE_PATH" pex-builder \
  uvx pex \
  -r dist/requirements.txt \
  -o dist/bin.pex \
  -e main \
  --sources-dir=. \
  --python-shebang '#!/usr/bin/env python3' \
  --scie eager \
  --scie-pbs-stripped
else
  docker run --rm -it -v "$(pwd):/build" -w /build/"$PACKAGE_PATH" pex-builder \
  uvx pex \
  -r dist/requirements.txt \
  -o dist/bin.pex \
  -e main \
  --python-shebang '#!/usr/bin/env python3' \
  --sources-dir=. \
  --scie eager \
  --scie-pbs-stripped
fi

chmod +x "$DIST_DIR"/bin

echo "output artifacts in $DIST_DIR:"
ls -lh "$DIST_DIR"
