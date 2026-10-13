#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
if [ "$#" -gt 1 ] || { [ "$#" -eq 1 ] && [ "$1" != '--capture-inputs' ]; }; then
  echo 'Only --capture-inputs is accepted; configure the qualified executables through ROUTEX_SMTP_BINARY and ROUTEX_SMTP_HELPER.' >&2
  exit 2
fi
exec node scripts/smtp-acceptance.mjs "$@"
