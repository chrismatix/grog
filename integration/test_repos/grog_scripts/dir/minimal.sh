#!/bin/bash

if [[ "$1" != "bar" ]]; then
  echo "expected 'bar'"
  exit 1
fi
