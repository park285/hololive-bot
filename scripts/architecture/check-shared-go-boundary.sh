#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

resolve_shared_go_dir() {
  local candidate="${SHARED_GO_WORKSPACE_PATH:-}"
  if [[ -z "${candidate}" ]]; then
    if [[ -d "${ROOT_DIR}/shared-go" ]]; then
      candidate="${ROOT_DIR}/shared-go"
    elif [[ -d "${ROOT_DIR}/../shared-go" ]]; then
      candidate="${ROOT_DIR}/../shared-go"
    fi
  fi
  if [[ ! -d "${candidate}" ]]; then
    echo "error: active shared-go dir not found" >&2
    exit 1
  fi

  printf '%s\n' "$(cd "${candidate}" && pwd)"
}

SHARED_GO_DIR="$(resolve_shared_go_dir)"

tmp_edges="$(mktemp)"
cleanup() {
  rm -f "${tmp_edges}"
}
trap cleanup EXIT

pushd "${SHARED_GO_DIR}" >/dev/null
GOWORK=off go list -f '{{if not .Standard}}{{.ImportPath}}{{range .Imports}} {{.}}{{end}}{{end}}' ./... \
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
