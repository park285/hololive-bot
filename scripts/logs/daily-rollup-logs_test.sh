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

# shellcheck disable=SC1090
source "${ROOT_DIR}/scripts/logs/daily-rollup-logs.sh"

# 회귀 4eef60aa: mv 직후 공격자가 recreate 경로에 symlink 를 심은 상황을 모사.
secret="${TMP_DIR}/secret"
printf 'SECRET' > "${secret}"
chmod 0600 "${secret}"
before_mode="$(stat -c '%a' "${secret}")"

reffile="${TMP_DIR}/ref"
printf '' > "${reffile}"
chmod 0640 "${reffile}"

tmp_dir="$(mktemp -d "${LOG_ROOT}/.rollup.XXXXXX")"
logpath="${LOG_ROOT}/svc.log"
ln -s "${secret}" "${logpath}"

recreate_empty_log "${logpath}" "${reffile}" "${tmp_dir}"

after_mode="$(stat -c '%a' "${secret}")"
if [[ "$(cat "${secret}")" == "SECRET" && "${after_mode}" == "${before_mode}" ]]; then
  pass "symlink target untouched (mode ${after_mode}, content intact)"
else
  record_fail "symlink target modified: mode ${before_mode}->${after_mode}, content=$(cat "${secret}")"
fi

if [[ -f "${logpath}" && ! -L "${logpath}" && ! -s "${logpath}" ]]; then
  pass "planted symlink replaced by a fresh empty regular file"
else
  record_fail "logpath is not a fresh regular file after recreate"
fi

tmp_dir2="$(mktemp -d "${LOG_ROOT}/.rollup.XXXXXX")"
freepath="${LOG_ROOT}/ok.log"
recreate_empty_log "${freepath}" "${reffile}" "${tmp_dir2}"
if [[ -f "${freepath}" && ! -L "${freepath}" ]]; then
  pass "recreate_empty_log created a regular file on a free path"
else
  record_fail "recreate_empty_log did not create a regular file"
fi

if (( failures > 0 )); then
  echo "FAILED: ${failures} check(s)"
  exit 1
fi
echo "all rollup symlink-guard checks passed (4eef60aa)"
