#!/usr/bin/env bash
set -euo pipefail

# Generate Python protobuf and gRPC stubs into lib/proto/pb/
# Requires: python with grpcio-tools installed (e.g. via `uv pip install grpcio-tools` or workspace dev group)

SCHEMA_DIR="schema"
OUT_DIR="pb"

mkdir -p "$OUT_DIR"

# Ensure packages exist
touch "$OUT_DIR/__init__.py"

uv run python -m grpc_tools.protoc \
	-I"$SCHEMA_DIR" \
	--python_out="$OUT_DIR" \
	--grpc_python_out="$OUT_DIR" \
	$SCHEMA_DIR/*.proto

echo "Protobuf stubs generated $OUT_DIR"
