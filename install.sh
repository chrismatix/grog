#!/usr/bin/env bash

set -euo pipefail

make build

install -m 755 dist/grog /usr/local/bin/grog
