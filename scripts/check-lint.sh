#!/usr/bin/env bash
# Run golangci-lint with development build tag.
# Prefers the binary in GOPATH/bin; falls back to whatever is on PATH.
set -euo pipefail

LINT_BIN="$(go env GOPATH)/bin/golangci-lint"
if [ ! -x "$LINT_BIN" ]; then
  # Fall back to whatever is on PATH (e.g. installed via system package manager)
  LINT_BIN="$(command -v golangci-lint)" || { echo "golangci-lint not found; run: go tool task update-tools"; exit 1; }
fi
"$LINT_BIN" run --build-tags=development ./cmd/... ./internal/... ./pkg/... ./website
