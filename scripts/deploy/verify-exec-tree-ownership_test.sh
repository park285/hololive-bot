#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERIFY="${ROOT_DIR}/scripts/deploy/verify-exec-tree-ownership.sh"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

failures=0
record_fail() { echo "[FAIL] $*" >&2; failures=$((failures + 1)); }
pass() { echo "[PASS] $*"; }

kapu_file="${TMP_DIR}/compose.sh"
printf '#!/bin/sh\n' > "${kapu_file}"
if "${VERIFY}" "${kapu_file}" >/dev/null 2>&1; then
  record_fail "non-root-owned file must be rejected (03e6dca8)"
else
  pass "non-root-owned file rejected"
fi

ww_file="${TMP_DIR}/world-writable"
printf 'x' > "${ww_file}"
chmod 0666 "${ww_file}"
if "${VERIFY}" "${ww_file}" >/dev/null 2>&1; then
  record_fail "writable file must be rejected"
else
  pass "writable file rejected"
fi

root_safe=""
for cand in /usr/bin/env /bin/true /usr/bin/true /bin/sh; do
  [[ -e "${cand}" ]] || continue
  cand_perms="$(printf '%04d' "$((10#$(stat -c '%a' "${cand}")))")"
  if [[ "$(stat -c '%u' "${cand}")" -eq 0 ]] \
     && (( ( ${cand_perms:3:1} & 2 ) == 0 )) \
     && (( ( ${cand_perms:2:1} & 2 ) == 0 )); then
    root_safe="${cand}"
    break
  fi
done
if [[ -z "${root_safe}" ]]; then
  echo "[SKIP] no root-owned reference file available for the pass case"
elif "${VERIFY}" "${root_safe}" >/dev/null 2>&1; then
  pass "root-owned non-writable file accepted (${root_safe})"
else
  record_fail "root-owned non-writable file wrongly rejected (${root_safe})"
fi

if (( failures > 0 )); then
  echo "FAILED: ${failures} check(s)"
  exit 1
fi
echo "all exec-tree ownership checks passed (03e6dca8)"
