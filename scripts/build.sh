#!/usr/bin/env bash
# Build the server binary for the current platform.
# Env overrides: BIN_DIR, APP_NAME.
set -euo pipefail

: "${BIN_DIR:=bin}"
: "${APP_NAME:=routex}"

GIT_COMMIT="$(git rev-parse --short HEAD)"
BUILD_TIME="$(TZ='Asia/Shanghai' date '+%Y-%m-%d-%H-%M-%S')"
LDFLAGS="-s -w -X main.CommitID=${GIT_COMMIT} -X main.BuildTime=${BUILD_TIME}"

mkdir -p "$BIN_DIR"
output="${BIN_DIR}/${APP_NAME}$(go env GOEXE)"
echo "Building ${output}"
CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$output" ./cmd/routex/
