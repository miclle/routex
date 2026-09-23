#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
project="routex-auth-test-$(date +%s)-$$-${RANDOM}"
compose=(docker compose -p "$project" -f compose.test.yaml)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/routex-auth-test.XXXXXXXX")
runner_pid=
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
  rm -rf "$fixture"
  exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

go build -trimpath -tags development -o "$fixture/routex" ./cmd/routex
"${compose[@]}" up --detach --wait --wait-timeout 180
postgres_address=$("${compose[@]}" port postgres 5432)
mysql_address=$("${compose[@]}" port mysql 3306)
export ROUTEX_TEST_POSTGRES_DSN="host=127.0.0.1 port=${postgres_address##*:} user=routex password=routex-test dbname=routex_test sslmode=disable"
export ROUTEX_TEST_MYSQL_DSN="routex:routex-test@tcp(${mysql_address})/routex_test?charset=utf8mb4&parseTime=True&loc=UTC"

node scripts/auth-lifecycle.mjs "$fixture/routex" "$fixture" &
runner_pid=$!
wait "$runner_pid"
runner_pid=
