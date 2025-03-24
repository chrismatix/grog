#!/bin/bash

members=$(uv run python ./scripts/get_workspaces.py)

echo $members

for dir in $members; do
    echo "Testing $dir..."
    (cd "$dir" && uv run pytest) || exit 1
done
