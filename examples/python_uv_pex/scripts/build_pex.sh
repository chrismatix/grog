#!/bin/bash

set -euo pipefail

# Check if the argument (local package path) is provided
if [ -z "$1" ]; then
  echo "Usage: $0 <local_package_path>"
  exit 1
fi

# Navigate to the specified package path
PACKAGE_PATH="$1"

DIST_DIR="$PACKAGE_PATH"/dist

# If we don't clear the target bin pex will keep
# adding to the old archive!
rm -r "$DIST_DIR" || echo "no dist dir found. creating"
echo "Cleared dist directory $DIST_DIR"

if [ -d "$PACKAGE_PATH" ]; then
  cd "$PACKAGE_PATH" || { echo "Failed to cd into $PACKAGE_PATH"; exit 1; }
else
  echo "Directory $PACKAGE_PATH does not exist."
  exit 1
fi



echo "Building the pex file for $PACKAGE_PATH"

# Generate the package specific requirements txt
uv pip compile pyproject.toml --universal -o dist/requirements.txt --quiet

# Build the pex
# TODO figure out how to do cross platform builds without docker
# At the moment this still leads to rather annoying issues with dependencies that
# do not have a pre-built whl for the target platform
# https://zameermanji.com/blog/2021/6/25/packaging-multi-platform-python-applications/

uvx pex \
-r dist/requirements.txt \
-o dist/bin.pex \
-e main \
--python-shebang '#!/usr/bin/env python3' \
--sources-dir=. \
--scie eager \
--scie-pbs-stripped

chmod +x dist/bin

echo "output artifacts in $DIST_DIR:"
ls -lh dist
