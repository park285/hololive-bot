#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
if [[ -d "${ROOT_DIR}/shared-go" ]]; then SHARED_GO_DIR="${ROOT_DIR}/shared-go"; else SHARED_GO_DIR="${ROOT_DIR}/../shared-go"; fi

tmp_hits="$(mktemp)"
cleanup() {
  rm -f "${tmp_hits}"
}
trap cleanup EXIT

grep -R -n --include='*.go' '"/internal/trigger/' "${ROOT_DIR}/hololive" "${SHARED_GO_DIR}" 2>/dev/null \
  | grep -v 'hololive-shared/pkg/contracts/trigger/routes_test.go' \
  > "${tmp_hits}" || true

if [[ -s "${tmp_hits}" ]]; then
  echo "FAIL: hardcoded trigger route found outside contracts" >&2
  cat "${tmp_hits}" >&2
  exit 1
fi

echo "OK: no hardcoded trigger route outside contracts"
