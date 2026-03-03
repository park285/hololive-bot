#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SHARED_GO_PKG_DIR="${ROOT_DIR}/shared-go/pkg"
ALLOWLIST_FILE="${1:-${ROOT_DIR}/docs/architecture/shared-go-package-allowlist.txt}"

if [[ ! -d "${SHARED_GO_PKG_DIR}" ]]; then
  echo "error: shared-go pkg dir not found: ${SHARED_GO_PKG_DIR}" >&2
  exit 1
fi

if [[ ! -f "${ALLOWLIST_FILE}" ]]; then
  echo "error: allowlist not found: ${ALLOWLIST_FILE}" >&2
  exit 1
fi

tmp_found="$(mktemp)"
tmp_allowed="$(mktemp)"
cleanup() {
  rm -f "${tmp_found}" "${tmp_allowed}"
}
trap cleanup EXIT

find "${SHARED_GO_PKG_DIR}" -type f -name '*.go' ! -name '*_test.go' -print \
  | sed "s#^${SHARED_GO_PKG_DIR}/##" \
  | xargs -r -n1 dirname \
  | sort -u > "${tmp_found}"

grep -vE '^\s*(#|$)' "${ALLOWLIST_FILE}" | sort -u > "${tmp_allowed}"

new_packages="$(comm -13 "${tmp_allowed}" "${tmp_found}" || true)"
stale_allowlist="$(comm -23 "${tmp_allowed}" "${tmp_found}" || true)"

if [[ -n "${new_packages}" ]]; then
  echo "FAIL: new shared-go packages detected (not in allowlist)" >&2
  echo "${new_packages}" >&2
  echo >&2
  echo "Update allowlist only when intentionally accepted:" >&2
  echo "  ${ALLOWLIST_FILE}" >&2
  exit 1
fi

count="$(wc -l < "${tmp_found}" | tr -d '[:space:]')"
echo "OK: no new shared-go packages (count: ${count})"

if [[ -n "${stale_allowlist}" ]]; then
  echo
  echo "Info: remove stale allowlist entries:"
  echo "${stale_allowlist}"
fi
