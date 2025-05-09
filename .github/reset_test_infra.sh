#!/bin/bash

set -euo pipefail

# Clean up remote test infrastructure

# 1. Delete all Docker images in the Artifact Registry
# 2. Delete the repository

REGISTRY_URL="us-central1-docker.pkg.dev/grog-457510/grog-cache"

echo "Listing Docker images in the Artifact Registry: ${REGISTRY_URL}"

# List all Docker images; the output is a list of image names.
# We assume that the repository holds Docker images
IMAGES=$(gcloud artifacts docker images list "${REGISTRY_URL}" --format="value(IMAGE)")

if [ -z "$IMAGES" ]; then
    echo "No images found in ${REGISTRY_URL}."
fi

for IMAGE in $IMAGES; do
    echo "Deleting all versions of the image: ${IMAGE}"
    # Delete the image and all its tags. The --quiet flag suppresses confirmation prompts.
    # Remove --delete-tags if you wish to control tag deletion separately.
    gcloud artifacts docker images delete "${IMAGE}" --quiet --delete-tags
    if [ $? -eq 0 ]; then
        echo "Successfully deleted image: ${IMAGE}"
    else
        echo "Failed to delete image: ${IMAGE}"
    fi
done

echo "Cleanup of Artifact Registry ${REGISTRY_URL} completed."

# Clean up GCS bucket
BUCKET_NAME="grog-test-cache"

echo "Listing objects in GCS bucket: ${BUCKET_NAME}"

# List all objects in the bucket
OBJECTS=$(gsutil ls "gs://${BUCKET_NAME}/**")

if [ -z "$OBJECTS" ]; then
    echo "No objects found in ${BUCKET_NAME}."
else
    echo "Deleting all objects in bucket: ${BUCKET_NAME}"
    # Delete all objects in the bucket. The -m flag enables parallel execution
    gsutil -m rm -r "gs://${BUCKET_NAME}/**"
    if [ $? -eq 0 ]; then
        echo "Successfully deleted all objects in bucket: ${BUCKET_NAME}"
    else
        echo "Failed to delete objects in bucket: ${BUCKET_NAME}"
    fi
fi

echo "Cleanup of GCS bucket ${BUCKET_NAME} completed."
