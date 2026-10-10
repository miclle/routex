#!/usr/bin/env bash
# Check Go formatting without modifying source files.
set -euo pipefail

unformatted_files="$(gofmt -l .)"
if [[ -n "$unformatted_files" ]]; then
  printf '%s\n' "$unformatted_files"
  printf '%s\n' "Go files are not formatted. Run 'go tool task lint' to fix."
  exit 1
fi
