#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}/scripts/scripttest"
export GOWORK=off
export GOFLAGS="${GOFLAGS:+${GOFLAGS} }-mod=readonly"

if (( $# == 0 )); then
    set -- ./...
fi

go test -p 1 -parallel 4 -count=1 "$@"
