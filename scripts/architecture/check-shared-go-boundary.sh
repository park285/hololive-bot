#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SHARED_GO_DIR="${ROOT_DIR}/shared-go"

if [[ ! -d "${SHARED_GO_DIR}" ]]; then
  echo "error: shared-go dir not found: ${SHARED_GO_DIR}" >&2
  exit 1
fi

tmp_edges="$(mktemp)"
cleanup() {
  rm -f "${tmp_edges}"
}
trap cleanup EXIT

pushd "${SHARED_GO_DIR}" >/dev/null
go list -f '{{if not .Standard}}{{.ImportPath}}{{range .Imports}} {{.}}{{end}}{{end}}' ./... \
  | awk '
      $1 != "" {
        from = $1
        for (i = 2; i <= NF; i++) {
          to = $i
          if (to ~ /^github.com\/kapu\/hololive-/) {
            printf "%s -> %s\n", from, to
          }
        }
      }
    ' \
  | sort -u > "${tmp_edges}"
popd >/dev/null

if [[ -s "${tmp_edges}" ]]; then
  echo "FAIL: shared-go must not import hololive modules" >&2
  cat "${tmp_edges}" >&2
  exit 1
fi

echo "OK: shared-go boundary check passed (no hololive module imports)"
