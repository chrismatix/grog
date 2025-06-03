#!/bin/bash

# Usage: ./release_notes.sh <tag1> <tag2>
# Example: ./release_notes.sh v1.0.0 v1.1.0

TAG1=$1
TAG2=$2
REPO_URL=https://github.com/chrismatix/grog

if [ -z "$TAG1" ] || [ -z "$TAG2" ] || [ -z "$REPO_URL" ]; then
  echo "Usage: $0 <tag1> <tag2>"
  exit 1
fi

git log --pretty=format:"- [\`%h\`]($REPO_URL/commit/%H) %s" "${TAG1}..${TAG2}"
