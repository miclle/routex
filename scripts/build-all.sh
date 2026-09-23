#!/usr/bin/env bash
# Cross-compile server binaries for all supported platforms.
# Env overrides: BIN_DIR, APP_NAME, PLATFORMS (space-separated GOOS/GOARCH pairs).
set -euo pipefail

: "${BIN_DIR:=bin}"
: "${APP_NAME:=routex}"
: "${PLATFORMS:=linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64}"

GIT_COMMIT="$(git rev-parse --short HEAD)"
BUILD_TIME="$(TZ='Asia/Shanghai' date '+%Y-%m-%d-%H-%M-%S')"
LDFLAGS="-s -w -X main.CommitID=${GIT_COMMIT} -X main.BuildTime=${BUILD_TIME}"

mkdir -p "$BIN_DIR"
for platform in $PLATFORMS; do
  goos="${platform%/*}"
  goarch="${platform#*/}"
  output="${BIN_DIR}/${APP_NAME}_${goos}_${goarch}"
  if [ "$goos" = "windows" ]; then
    output="${output}.exe"
  fi
  echo "Building ${output}"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "$LDFLAGS" -o "$output" ./cmd/routex/
done
