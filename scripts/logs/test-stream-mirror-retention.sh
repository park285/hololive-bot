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

source "${ROOT_DIR}/scripts/logs/lib/stream.sh"

write_chunk() {
  local path="$1"
  local label="$2"
  printf '%s\n' "${label}" > "${path}"
}

log_file="${TMP_DIR}/mirror/bot.log"
chunk_file="${TMP_DIR}/chunk"
mkdir -p "${TMP_DIR}/mirror"

STREAM_MIRROR_MAX_BYTES=20 STREAM_MIRROR_MAX_ROTATIONS=2
export STREAM_MIRROR_MAX_BYTES STREAM_MIRROR_MAX_ROTATIONS

write_chunk "${chunk_file}" "first-entry"
stream_mirror_append_file "${log_file}" "${chunk_file}"
[[ "$(cat "${log_file}")" == "first-entry" ]] || fail "first chunk should be active"

write_chunk "${chunk_file}" "second-entry"
stream_mirror_append_file "${log_file}" "${chunk_file}"
[[ "$(cat "${log_file}")" == "second-entry" ]] || fail "second chunk should be active after rotation"
[[ "$(cat "${log_file}.1")" == "first-entry" ]] || fail "first rotation should contain prior active log"

write_chunk "${chunk_file}" "third-entry"
stream_mirror_append_file "${log_file}" "${chunk_file}"
[[ "$(cat "${log_file}")" == "third-entry" ]] || fail "third chunk should be active after rotation"
[[ "$(cat "${log_file}.1")" == "second-entry" ]] || fail "latest rotation should contain second chunk"
[[ "$(cat "${log_file}.2")" == "first-entry" ]] || fail "second rotation should contain first chunk"

write_chunk "${chunk_file}" "fourth-entry"
stream_mirror_append_file "${log_file}" "${chunk_file}"
[[ ! -e "${log_file}.3" ]] || fail "rotation should prune beyond retention"
[[ "$(cat "${log_file}.2")" == "second-entry" ]] || fail "oldest retained rotation should move forward"

pass "stream mirror rotates and prunes retained logs"
