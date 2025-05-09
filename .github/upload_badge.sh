#!/bin/bash
set -e

# Variables (update these with your actual GCS bucket name and destination key)
BUCKET_NAME="grog-assets"
OBJECT_KEY="github/coverage.svg"

# Path to the coverage file
COVERAGE_FILE="coverdata/coverage_overview.out"

# Check if the coverage file exists
if [ ! -f "$COVERAGE_FILE" ]; then
  echo "Error: Coverage file $COVERAGE_FILE not found."
  exit 1
fi

# Extract the last word from the file (assumed to be the coverage percentage)
coverage=$(awk 'END {print $NF}' "$COVERAGE_FILE")

# Encode the '%' if present
encoded_coverage=$(echo "$coverage" | sed 's/%/%25/g')

# Construct the shields.io badge URL.
badge_url="https://img.shields.io/badge/coverage-${encoded_coverage}-brightgreen.svg"

echo "Downloading badge from: $badge_url"

# Download the SVG badge (saved as badge.svg)
curl -sSfL "$badge_url" -o badge.svg

if [ $? -ne 0 ]; then
  echo "Error: Failed to download badge."
  exit 1
fi

echo "Badge downloaded successfully."

# Upload badge.svg to the specified GCS bucket using gsutil.
echo "Uploading badge.svg to gs://${BUCKET_NAME}/${OBJECT_KEY}"
gsutil cp badge.svg gs://${BUCKET_NAME}/${OBJECT_KEY}

if [ $? -ne 0 ]; then
  echo "Error: Failed to upload badge to gs://${BUCKET_NAME}/${OBJECT_KEY}."
  exit 1
fi

echo "Successfully uploaded badge to gs://${BUCKET_NAME}/${OBJECT_KEY}."
