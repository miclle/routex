#!/usr/bin/env bash
# Install GolangCI-Lint if missing.
# Task, reflex, staticcheck, and actionlint are managed by go.mod and invoked with go tool.
# Idempotent: existing binaries are left alone.
set -euo pipefail

GOLANGCI_LINT_VERSION="v2.13.2"

GOBIN="$(go env GOPATH)/bin"
mkdir -p "$GOBIN"
if [ ! -x "$GOBIN/golangci-lint" ]; then
  GOBIN="$GOBIN" GO111MODULE=on go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}"
fi
