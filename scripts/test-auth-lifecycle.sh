#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
umask 077
project="routex-auth-test-$(date +%s)-$$-${RANDOM}"
compose=(docker compose -p "$project" -f compose.test.yaml)
# Retain original outcomes and journals privately; remove only the owned binary.
evidence_parent="$HOME/.local/share/routex/acceptance/auth-lifecycle"
mkdir -p "$evidence_parent"
fixture=$(mktemp -d "$evidence_parent/run.XXXXXXXX")
printf '%s\n' "$project" > "$fixture/compose-project"
runner_pid=
project_absent() {
  local containers networks
  containers=$(docker ps -aq --filter "label=com.docker.compose.project=$project") || return 1
  networks=$(docker network ls -q --filter "label=com.docker.compose.project=$project") || return 1
  [ -z "$containers" ] && [ -z "$networks" ]
}
cleanup() {
  result=$?
  trap - EXIT
  if [ -n "$runner_pid" ]; then
    kill -TERM "$runner_pid" 2>/dev/null || true
    wait "$runner_pid" 2>/dev/null || true
  fi
  if ! "${compose[@]}" down --timeout 10; then
    echo "Failed to clean up authentication test project $project" >&2
    if [ "$result" -eq 0 ]; then result=1; fi
  fi
  if ! project_absent; then
    echo "Authentication test resources remain for $project" >&2
    result=1
  fi
  rm -f "$fixture/routex"
  printf '%s\n' "$result" > "$fixture/original-exit-code"
  printf 'Authentication lifecycle evidence: %s (exit %s)\n' "$fixture" "$result"
  exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

go build -trimpath -tags development -o "$fixture/routex" ./cmd/routex
node --input-type=module - "$fixture" <<'JS'
import { readFileSync, writeFileSync } from 'node:fs'
import { createHash } from 'node:crypto'
import { join } from 'node:path'
const files = ['scripts/auth-lifecycle.mjs', 'scripts/auth-lifecycle-offboarding.mjs', 'scripts/test-auth-lifecycle.sh', join(process.argv[2], 'routex')]
writeFileSync(join(process.argv[2], 'artifact-hashes.json'), JSON.stringify(Object.fromEntries(files.map((file) => [file, createHash('sha256').update(readFileSync(file)).digest('hex')])), null, 2), { mode: 0o600 })
JS
for driver in postgres mysql; do
  # Only one heavy database profile is active; fully close it before the next.
  "${compose[@]}" up --detach --wait --wait-timeout 180 "$driver"
  if [ "$driver" = postgres ]; then
    address=$("${compose[@]}" port postgres 5432)
    export ROUTEX_TEST_POSTGRES_DSN="host=127.0.0.1 port=${address##*:} user=routex password=routex-test dbname=routex_test sslmode=disable"
  else
    address=$("${compose[@]}" port mysql 3306)
    export ROUTEX_TEST_MYSQL_DSN="routex:routex-test@tcp(${address})/routex_test?charset=utf8mb4&parseTime=True&loc=UTC"
  fi
  node scripts/auth-lifecycle.mjs "$fixture/routex" "$fixture" "$driver" > "$fixture/$driver.log" 2>&1 &
  runner_pid=$!
  printf '%s\n' "$runner_pid" > "$fixture/$driver-original-pid"
  if wait "$runner_pid"; then result=0; else result=$?; fi
  runner_pid=
  printf '%s\n' "$result" > "$fixture/$driver-original-exit-code"
  cat "$fixture/$driver.log"
  if [ "$result" -ne 0 ]; then exit "$result"; fi
  "${compose[@]}" down --timeout 10
  if ! project_absent; then
    echo "Previous authentication test profile did not close" >&2
    exit 1
  fi
done
