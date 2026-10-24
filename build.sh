#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
export CGO_ENABLED=0
exec go tool mygo build "$@"
