#!/usr/bin/env bash
# Verify go.mod and go.sum are tidy without modifying working copies.
# Exits non-zero if `go mod tidy` would change anything.
set -euo pipefail

cp go.mod go.mod.bak
cp go.sum go.sum.bak
trap 'mv go.mod.bak go.mod; mv go.sum.bak go.sum' EXIT

go mod tidy

if ! diff -q go.mod go.mod.bak > /dev/null || ! diff -q go.sum go.sum.bak > /dev/null; then
  echo "go.mod or go.sum is not tidy. Run 'go tool task lint' to fix."
  exit 1
fi
