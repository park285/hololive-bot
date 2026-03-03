#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
ADMIN_DIR="${ROOT_DIR}/hololive/hololive-admin"
KAKAO_DIR="${ROOT_DIR}/hololive/hololive-kakao-bot-go"
ALLOWLIST_FILE="${1:-${ROOT_DIR}/docs/architecture/admin-kakao-duplicate-allowlist.txt}"
MAX_COUNT_FILE="${2:-${ROOT_DIR}/docs/architecture/admin-kakao-duplicate-max.txt}"

SCOPES=(
  "internal/server"
  "internal/app"
  "internal/service/auth"
)

if [[ ! -f "${ALLOWLIST_FILE}" ]]; then
  echo "error: allowlist not found: ${ALLOWLIST_FILE}" >&2
  exit 1
fi

if [[ ! -f "${MAX_COUNT_FILE}" ]]; then
  echo "error: max count file not found: ${MAX_COUNT_FILE}" >&2
  exit 1
fi

tmp_current="$(mktemp)"
tmp_allowed="$(mktemp)"
cleanup() {
  rm -f "${tmp_current}" "${tmp_allowed}"
}
trap cleanup EXIT

{
  for scope in "${SCOPES[@]}"; do
    comm -12 \
      <(find "${ADMIN_DIR}/${scope}" -type f -name '*.go' -printf '%P\n' | sort) \
      <(find "${KAKAO_DIR}/${scope}" -type f -name '*.go' -printf '%P\n' | sort) \
      | sed "s#^#${scope}/#"
  done
} | sort -u > "${tmp_current}"

{ grep -Ev '^[[:space:]]*($|#)' "${ALLOWLIST_FILE}" || true; } | sed 's/[[:space:]]*$//' | sort -u > "${tmp_allowed}"

new_duplicates="$(comm -23 "${tmp_current}" "${tmp_allowed}" || true)"
resolved_duplicates="$(comm -13 "${tmp_current}" "${tmp_allowed}" || true)"
current_count="$(wc -l < "${tmp_current}" | tr -d '[:space:]')"
max_allowed_count="$(
  { grep -Ev '^[[:space:]]*($|#)' "${MAX_COUNT_FILE}" || true; } \
    | head -n1 \
    | tr -d '[:space:]'
)"

if [[ -z "${max_allowed_count}" || ! "${max_allowed_count}" =~ ^[0-9]+$ ]]; then
  echo "error: invalid max count value in ${MAX_COUNT_FILE}" >&2
  exit 1
fi

if [[ -n "${new_duplicates}" ]]; then
  echo "FAIL: new admin↔kakao duplicate files detected (${current_count} current)" >&2
  echo "${new_duplicates}" >&2
  echo
  echo "Update allowlist only when duplication is intentionally accepted:"
  echo "  ${ALLOWLIST_FILE}"
  exit 1
fi

if (( current_count > max_allowed_count )); then
  echo "FAIL: admin↔kakao duplicate count exceeded max (${current_count} > ${max_allowed_count})" >&2
  echo "Reduce duplicated files before merging, then sync:"
  echo "  ${ALLOWLIST_FILE}"
  echo "  ${MAX_COUNT_FILE}"
  exit 1
fi

echo "OK: no new admin↔kakao duplicate files"
echo "Current duplicate count: ${current_count} (max: ${max_allowed_count})"

if [[ -n "${resolved_duplicates}" ]]; then
  echo
  echo "Info: remove stale allowlist entries:"
  echo "${resolved_duplicates}"
fi
