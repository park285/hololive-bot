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

tmp_dir3="$(mktemp -d "${LOG_ROLLUP_STATE_DIR}/.rollup.XXXXXX")"
hardlink_path="${LOG_ROOT}/hardlink.log"
hardlink_alias="${TMP_DIR}/hardlink-alias.log"
printf 'hardlink-content\n' > "${hardlink_path}"
ln "${hardlink_path}" "${hardlink_alias}"
if snapshot_and_truncate_log "${hardlink_path}" "${tmp_dir3}/hardlink.log" >/dev/null 2>&1; then
  record_fail "snapshot_and_truncate_log accepted a multi-link inode"
else
  rc=$?
  if [[ "${rc}" -eq 77 && "$(cat "${hardlink_path}")" == "hardlink-content" && "$(cat "${hardlink_alias}")" == "hardlink-content" ]]; then
    pass "multi-link copytruncate refused; both links preserved"
  else
    record_fail "multi-link refusal rc=${rc}; original or alias changed"
  fi
fi
rm -f -- "${hardlink_alias}" "${hardlink_path}"

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

bb_root="${TMP_DIR}/blackbox"
mkdir -p "${bb_root}/logs/remote/osaka"
printf 'bot yesterday\n' > "${bb_root}/logs/bot.log"
printf 'producer mirror\n' > "${bb_root}/logs/remote/osaka/youtube-producer.log"
ln -s remote/osaka/youtube-producer.log "${bb_root}/logs/youtube-producer.log"

bb_archive="${bb_root}/logs/archive/bot-2026-05-20.log.tar.gz"
if LOG_ROOT="${bb_root}/logs" LOG_ROLLUP_STATE_DIR="${bb_root}/state" LOG_ROLLUP_DATE=2026-05-20 \
    "${ROOT_DIR}/scripts/logs/daily-rollup-logs.sh" once >/dev/null; then
  if [[ -f "${bb_archive}" && ! -L "${bb_archive}" \
      && ! -s "${bb_root}/logs/bot.log" \
      && "$(cat "${bb_root}/logs/remote/osaka/youtube-producer.log")" == "producer mirror" \
      && "$(tar -xOzf "${bb_archive}" bot.log 2>/dev/null)" == "bot yesterday" ]]; then
    pass "once mode archives regular log content, truncates active log, skips symlinked mirror"
  else
    record_fail "once mode archive/truncate/mirror-skip assertions failed"
  fi
else
  record_fail "once mode rollup failed"
fi

if LOG_ROOT="${bb_root}/logs" LOG_ROLLUP_STATE_DIR="${bb_root}/state" LOG_ROLLUP_DATE=2026-05-20 \
    "${ROOT_DIR}/scripts/logs/daily-rollup-logs.sh" once >/dev/null && [[ -f "${bb_archive}" ]]; then
  pass "once mode rerun is idempotent; archive remains"
else
  record_fail "once mode idempotent rerun failed or archive missing"
fi

printf 'producer regular\n' > "${bb_root}/logs/producer.log"
printf 'legacy regular\n' > "${bb_root}/logs/legacy.log"
if LOG_ROOT="${bb_root}/logs" LOG_ROLLUP_STATE_DIR="${bb_root}/state" LOG_ROLLUP_DATE=2026-05-21 \
    LOG_ROLLUP_FILES=producer.log \
    "${ROOT_DIR}/scripts/logs/daily-rollup-logs.sh" once >/dev/null; then
  if [[ -f "${bb_root}/logs/archive/producer-2026-05-21.log.tar.gz" \
      && "$(cat "${bb_root}/logs/legacy.log")" == "legacy regular" ]]; then
    pass "once mode LOG_ROLLUP_FILES restricts target log files; unmatched log untouched"
  else
    record_fail "filtered rollup archived wrong set or touched unmatched log"
  fi
else
  record_fail "filtered rollup failed"
fi

if (( failures > 0 )); then
  echo "FAILED: ${failures} check(s)"
  exit 1
fi
echo "all rollup checks passed (symlink guards ported from hololive-bot 4eef60aa + once-mode behavior)"
