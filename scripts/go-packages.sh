#!/usr/bin/env bash
# Print Go packages for this module, excluding Go files shipped inside npm deps.
set -euo pipefail

go list -tags development ./... | grep -v '/node_modules/'
