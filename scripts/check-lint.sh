#!/usr/bin/env bash
# Use the project's versioned linter, independently of global GOPATH installs.
set -euo pipefail

LINT_BIN="$("$(dirname "$0")/install-tools.sh" --print-path)"
"$LINT_BIN" run --build-tags=development ./cmd/... ./internal/... ./pkg/... ./website
