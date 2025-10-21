#!/bin/bash

# @grog
# name: minimal_name
# dependencies:
#   - //:a_build_target

if [[ "$1" != "bar" ]]; then
  echo "expected 'bar'"
  exit 1
fi
