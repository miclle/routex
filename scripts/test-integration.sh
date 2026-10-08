#!/usr/bin/env bash
set -euo pipefail
umask 077

cd "$(dirname "$0")/.."
# Never share a project or storage with the developer's database or another run.
project="routex-test-$(date +%s)-$$-${RANDOM}"
private_dir=$(mktemp -d "${TMPDIR:-/tmp}/routex-test-runner.XXXXXXXX")
chmod 700 "$private_dir"
# Compile only the stdlib supervisor. No go-run wrapper can orphan workers when
# the entry point receives a signal; exec gives the supervisor this exact PID.
trap 'rm -f "$private_dir/runner" >> "$private_dir/build.log" 2>&1' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# Keep compiler diagnostics private too, before the supervisor owns any database.
go build -trimpath -o "$private_dir/runner" ./scripts/test-integration-runner > "$private_dir/build.log" 2>&1
exec "$private_dir/runner" -project "$project" -private-dir "$private_dir"
