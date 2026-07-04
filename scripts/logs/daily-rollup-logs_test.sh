#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

failures=0
record_fail() { echo "[FAIL] $*" >&2; failures=$((failures + 1)); }
pass() { echo "[PASS] $*"; }

LOG_ROOT="${TMP_DIR}/logs"
mkdir -p "${LOG_ROOT}"
export LOG_ROOT
LOG_ROLLUP_STATE_DIR="${TMP_DIR}/state"
mkdir -p "${LOG_ROLLUP_STATE_DIR}"
export LOG_ROLLUP_STATE_DIR

# shellcheck disable=SC1090
source "${ROOT_DIR}/scripts/logs/daily-rollup-logs.sh"

secret="${TMP_DIR}/secret"
printf 'SECRET' > "${secret}"
chmod 0600 "${secret}"
before_mode="$(stat -c '%a' "${secret}")"

reffile="${TMP_DIR}/ref"
printf '' > "${reffile}"
chmod 0640 "${reffile}"

tmp_dir="$(mktemp -d "${LOG_ROLLUP_STATE_DIR}/.rollup.XXXXXX")"
logpath="${LOG_ROOT}/svc.log"
ln -s "${secret}" "${logpath}"

if snapshot_and_truncate_log "${logpath}" "${tmp_dir}/svc.log" >/dev/null 2>&1; then
  record_fail "snapshot_and_truncate_log followed a symlink"
fi

after_mode="$(stat -c '%a' "${secret}")"
if [[ "$(cat "${secret}")" == "SECRET" && "${after_mode}" == "${before_mode}" ]]; then
  pass "symlink target untouched (mode ${after_mode}, content intact)"
else
  record_fail "symlink target modified: mode ${before_mode}->${after_mode}, content=$(cat "${secret}")"
fi

if [[ -L "${logpath}" ]]; then
  pass "planted symlink left untouched after skipped snapshot"
else
  record_fail "logpath symlink was unexpectedly replaced"
fi

tmp_dir2="$(mktemp -d "${LOG_ROLLUP_STATE_DIR}/.rollup.XXXXXX")"
freepath="${LOG_ROOT}/ok.log"
printf 'before-rollup\n' > "${freepath}"
exec {held_fd}>>"${freepath}"
printf 'held-before\n' >&${held_fd}
snapshot_and_truncate_log "${freepath}" "${tmp_dir2}/ok.log"
printf 'held-after\n' >&${held_fd}
exec {held_fd}>&-
if grep -q 'held-before' "${tmp_dir2}/ok.log" && grep -q 'held-after' "${freepath}"; then
  pass "open file descriptor keeps writing to visible log after snapshot truncate"
else
  record_fail "open file descriptor did not keep writing to visible log after snapshot truncate"
fi

lock_target="${TMP_DIR}/lock-secret"
printf 'LOCKSECRET' > "${lock_target}"
ln -s "${lock_target}" "${LOG_ROOT}/.daily-rollup.lock"
if ( rollup_once ) >/dev/null 2>&1 && [[ "$(cat "${lock_target}")" == "LOCKSECRET" ]]; then
  pass "legacy log-root lock symlink ignored; lock target untouched"
else
  record_fail "legacy log-root lock symlink was opened or blocked safe rollup"
fi

lock_dir_original="${LOCK_DIR}"
lock_dir_target="${TMP_DIR}/lock-dir-target"
mkdir -p "${lock_dir_target}"
LOCK_DIR="${TMP_DIR}/planted-lock-dir"
ln -s "${lock_dir_target}" "${LOCK_DIR}"
if ( rollup_once ) >/dev/null 2>&1; then
  record_fail "rollup_once ran despite a symlinked state lock dir"
elif [[ -d "${lock_dir_target}" ]]; then
  pass "symlinked state lock dir refused; target untouched"
else
  record_fail "state lock target was modified"
fi
LOCK_DIR="${lock_dir_original}"

ROLLUP_DATE="2026-06-20"
archive_dir="${LOG_ROOT}/archive"
mkdir -p "${archive_dir}"

archive_secret="${TMP_DIR}/archive-secret"
printf 'ARCHIVESECRET' > "${archive_secret}"
printf 'archive me\n' > "${LOG_ROOT}/evil.log"
ln -s "${archive_secret}" "${archive_dir}/evil-2026-06-20.log.tar.gz"
if ( rollup_once ) >/dev/null 2>&1 && [[ "$(cat "${archive_secret}")" == "ARCHIVESECRET" ]]; then
  pass "pre-existing archive symlink skipped; target untouched"
else
  record_fail "archive symlink target was modified"
fi

printf 'safe log\n' > "${LOG_ROOT}/safe.log"
if ( rollup_once ) >/dev/null 2>&1; then
  if find "${LOG_ROOT}" -maxdepth 1 -name '.daily-rollup-*' | grep -q .; then
    record_fail "rollup staging leaked into LOG_ROOT"
  elif [[ -f "${archive_dir}/safe-2026-06-20.log.tar.gz" && ! -L "${archive_dir}/safe-2026-06-20.log.tar.gz" ]]; then
    pass "rollup staged outside LOG_ROOT and published a regular archive"
  else
    record_fail "safe archive missing or not regular"
  fi
else
  record_fail "safe rollup failed"
fi

if (( failures > 0 )); then
  echo "FAILED: ${failures} check(s)"
  exit 1
fi
echo "all rollup symlink-guard checks passed (ported from hololive-bot 4eef60aa)"
