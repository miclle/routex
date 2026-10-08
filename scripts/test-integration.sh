#!/usr/bin/env bash
set -euo pipefail
umask 077

cd "$(dirname "$0")/.."
# Never share a project or storage with the developer's database or another run.
project="routex-test-$(date +%s)-$$-${RANDOM}"
if [ -n "${ROUTEX_TEST_RUNNER_PRIVATE_DIR:-}" ]; then
  private_dir="$ROUTEX_TEST_RUNNER_PRIVATE_DIR"
  case "$private_dir" in
    /*) ;;
    *) echo "The integration artifact directory must be absolute." >&2; exit 1 ;;
  esac
  case "$private_dir" in
    */../*|*/./*|*/..|*/.|*/|*//*|*[[:space:]]*)
      echo "The integration artifact directory must be canonical." >&2
      exit 1
      ;;
  esac
  private_parent=$(dirname "$private_dir")
  physical_parent=$(cd -- "$private_parent" 2>/dev/null && pwd -P) || exit 1
  if [ "$physical_parent" != "$private_parent" ] || ! mkdir -m 700 "$private_dir" 2>/dev/null; then
    echo "The integration artifact directory must be new and have a physical parent." >&2
    exit 1
  fi
else
  private_dir=$(mktemp -d "${TMPDIR:-/tmp}/routex-test-runner.XXXXXXXX")
fi
chmod 700 "$private_dir"
# Compile only the stdlib supervisor. No go-run wrapper can orphan workers when
# the entry point receives a signal; exec gives the supervisor this exact PID.
trap 'rm -f "$private_dir/runner" >> "$private_dir/build.log" 2>&1' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# Keep compiler diagnostics private too, before the supervisor owns any database.
go build -trimpath -o "$private_dir/runner" ./scripts/test-integration-runner > "$private_dir/build.log" 2>&1
exec "$private_dir/runner" -project "$project" -private-dir "$private_dir"
