#!/usr/bin/env bash
set -euo pipefail
umask 077

cd "$(dirname "$0")/.."
# Inputs must refer to the exact independently qualified production source.
: "${ROUTEX_ACCEPTANCE_BINARY:?Set an absolute production executable path}"
: "${ROUTEX_ACCEPTANCE_HELPER:?Set an absolute fixture helper executable path}"
if [ "${1:-}" != "--capture-inputs" ]; then
  : "${ROUTEX_ACCEPTANCE_QUALIFICATION:?Set an absolute qualification receipt path}"
fi
# No compilation, image pull, shared development service or recursive shell
# process survives this entry point. The controller owns bounded detached groups.
exec node ./scripts/storage-native-acceptance.mjs "$@"
