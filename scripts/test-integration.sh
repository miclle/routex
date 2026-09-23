#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
# Never share a project or storage with the developer's database or another run.
project="routex-test-$(date +%s)-$$-${RANDOM}"
compose=(docker compose -p "$project" -f compose.test.yaml)
cleanup() {
  result=$?
  trap - EXIT
  if [ "$result" -ne 0 ]; then
    "${compose[@]}" logs --no-color || true
  fi
  if ! "${compose[@]}" down --timeout 10; then
    echo "Failed to clean up integration project $project" >&2
    if [ "$result" -eq 0 ]; then result=1; fi
  fi
  exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

"${compose[@]}" up --detach --wait --wait-timeout 180
postgres_address=$("${compose[@]}" port postgres 5432)
mysql_address=$("${compose[@]}" port mysql 3306)
export ROUTEX_TEST_POSTGRES_DSN="host=127.0.0.1 port=${postgres_address##*:} user=routex password=routex-test dbname=routex_test sslmode=disable"
export ROUTEX_TEST_MYSQL_DSN="routex:routex-test@tcp(${mysql_address})/routex_test?charset=utf8mb4&parseTime=True&loc=UTC"

go test -trimpath -race -count=1 -tags development ./internal/routex/...
