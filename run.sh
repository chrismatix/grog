#!/usr/bin/env bash

set -euo pipefail

make build

grog="$(pwd)/dist/grog"

cd "$1" && shift
$grog "$@"
