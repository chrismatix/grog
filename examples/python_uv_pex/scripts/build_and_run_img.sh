#!/bin/bash

set -euo pipefail

if [ -z "$1" ]; then
  echo "Usage: $0 <local_package_path>"
  exit 1
fi


./scripts/build_pex.sh "$1"

earthly ./"$1"+build

docker run -it --rm --name "$1" "$1"
