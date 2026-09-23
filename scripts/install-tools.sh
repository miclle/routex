#!/usr/bin/env bash
# Keep the pinned linter local to this checkout; other projects may install a
# different major version in GOPATH/bin while parallel work is running.
set -euo pipefail

GOLANGCI_LINT_VERSION="v2.13.2"
project_dir=$(cd "$(dirname "$0")/.." && pwd)
tools_dir="$project_dir/bin/tools"
lint_binary="$tools_dir/golangci-lint-${GOLANGCI_LINT_VERSION}"
mkdir -p "$tools_dir"
if [ ! -x "$lint_binary" ]; then
  install_dir=$(mktemp -d "$tools_dir/install.XXXXXXXX")
  trap 'rm -rf "$install_dir"' EXIT
  GOBIN="$install_dir" GO111MODULE=on go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}"
  mv "$install_dir/golangci-lint" "$lint_binary"
fi
if [ "${1:-}" = "--print-path" ]; then
  printf '%s\n' "$lint_binary"
fi
