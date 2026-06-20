#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

pass() {
  echo "[PASS] $*"
}

source "${ROOT_DIR}/scripts/logs/lib/query.sh"

PAYLOAD_BYTES=$((256 * 1024))

compose_query_output() {
  local svc="$1"
  if [[ "${svc}" == "bot" ]]; then
    head -c "${PAYLOAD_BYTES}" /dev/zero | tr '\0' 'X'
    printf '\n'
  fi
}

REPO_ROOT="${TMP_DIR}"
mirror_dir="${REPO_ROOT}/logs/mirror"
log_file="${mirror_dir}/bot.log"

DUMP_ROTATE_BYTES=$((64 * 1024))
DUMP_MAX_BYTES=$((48 * 1024))
export ENABLE_LOG_MIRROR=1 DUMP_ROTATE_BYTES DUMP_MAX_BYTES

cmd_dump >/dev/null 2>&1 || fail "cmd_dump must not error"

[[ -f "${log_file}" ]] || fail "active log must exist after dump"
active_size="$(stat -c%s "${log_file}")"
[[ "${active_size}" -le "${DUMP_MAX_BYTES}" ]] \
  || fail "single oversized dump must be capped at DUMP_MAX_BYTES, got ${active_size}"

cmd_dump >/dev/null 2>&1 || fail "second cmd_dump must not error"

[[ -f "${log_file}.1" ]] \
  || fail "destination + capped payload over DUMP_ROTATE_BYTES must rotate within the same cycle"

active_size="$(stat -c%s "${log_file}")"
[[ "${active_size}" -le "${DUMP_MAX_BYTES}" ]] \
  || fail "post-rotation active log must stay bounded, got ${active_size}"

pass "hb99: single dump does not bypass rotation (dc2bec31)"
