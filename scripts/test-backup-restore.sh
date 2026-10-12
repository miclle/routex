#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
if [ "$#" -ne 0 ] || [ -z "${ROUTEX_BACKUP_BINARY:-}" ]; then
  echo 'Set ROUTEX_BACKUP_BINARY to an already qualified default production binary; no arguments are accepted.' >&2
  exit 2
fi
exec node scripts/backup-restore.mjs
