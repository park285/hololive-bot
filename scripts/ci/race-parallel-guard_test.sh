#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LOCAL_CI="${ROOT_DIR}/scripts/ci/local-ci.sh"

failures=0
record_fail() { echo "[FAIL] $*" >&2; failures=$((failures + 1)); }
pass() { echo "[PASS] $*"; }

if grep -Eq 'RACE_TEST_PARALLEL.*=~.*\^\[0-9\]\+\$' "${LOCAL_CI}"; then
  pass "local-ci.sh validates RACE_TEST_PARALLEL as integer before arithmetic (82cbfe75)"
else
  record_fail "local-ci.sh missing RACE_TEST_PARALLEL integer guard (82cbfe75)"
fi

canary="$(mktemp -u)"
payload='x[$(touch '"${canary}"')]'

( (( ${payload} < 2 )) ) 2>/dev/null || true
if [[ -e "${canary}" ]]; then
  pass "unguarded arithmetic executes injected payload (vuln reproduced)"
  rm -f "${canary}"
else
  echo "[SKIP] arithmetic injection not reproduced by this bash build"
fi

if [[ "${payload}" =~ ^[0-9]+$ ]]; then
  record_fail "integer guard wrongly accepted an injection payload"
else
  ( [[ "${payload}" =~ ^[0-9]+$ ]] && (( ${payload} < 2 )) ) 2>/dev/null || true
  if [[ -e "${canary}" ]]; then
    record_fail "guarded path still executed the payload"
    rm -f "${canary}"
  else
    pass "integer guard blocks injection (canary not created)"
  fi
fi

valid_payload="5"
if [[ "${valid_payload}" =~ ^[0-9]+$ ]]; then
  pass "integer guard accepts a valid value"
else
  record_fail "integer guard rejected a valid integer"
fi

if (( failures > 0 )); then
  echo "FAILED: ${failures} check(s)"
  exit 1
fi
echo "all race-parallel guard checks passed (82cbfe75)"
