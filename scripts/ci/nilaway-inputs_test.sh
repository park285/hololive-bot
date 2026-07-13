#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/nilaway-inputs.sh"

failures=0

expect_valid_parallel() {
  local value="$1"
  if validate_nilaway_parallel "${value}" >/dev/null 2>&1; then
    printf '[PASS] NILAWAY_PARALLEL accepts %q\n' "${value}"
  else
    printf '[FAIL] NILAWAY_PARALLEL rejected %q\n' "${value}" >&2
    failures=$((failures + 1))
  fi
}

expect_invalid_parallel() {
  local value="$1"
  if validate_nilaway_parallel "${value}" >/dev/null 2>&1; then
    printf '[FAIL] NILAWAY_PARALLEL accepted %q\n' "${value}" >&2
    failures=$((failures + 1))
  else
    printf '[PASS] NILAWAY_PARALLEL rejects %q\n' "${value}"
  fi
}

expect_valid_memory_limit() {
  local value="$1"
  if validate_nilaway_gomemlimit "${value}" >/dev/null 2>&1; then
    printf '[PASS] NILAWAY_GOMEMLIMIT accepts %q\n' "${value}"
  else
    printf '[FAIL] NILAWAY_GOMEMLIMIT rejected %q\n' "${value}" >&2
    failures=$((failures + 1))
  fi
}

expect_invalid_memory_limit() {
  local value="$1"
  if validate_nilaway_gomemlimit "${value}" >/dev/null 2>&1; then
    printf '[FAIL] NILAWAY_GOMEMLIMIT accepted %q\n' "${value}" >&2
    failures=$((failures + 1))
  else
    printf '[PASS] NILAWAY_GOMEMLIMIT rejects %q\n' "${value}"
  fi
}

expect_valid_parallel 1
expect_valid_parallel 2
for value in 0 3 01 -1 1.5 abc ''; do
  expect_invalid_parallel "${value}"
done

for value in 0 123 123B 1KiB 512MiB 10GiB 1TiB off; do
  expect_valid_memory_limit "${value}"
done
dollar='$'
for value in '' 10GB 1KB 1Mi 1.5GiB -1 '10GiB; touch /tmp/pwn' "10GiB${dollar}(id)"; do
  expect_invalid_memory_limit "${value}"
done

canary="$(mktemp -u)"
parallel_payload="x[${dollar}(touch ${canary})]"
memory_payload='10GiB; touch '"${canary}"
expect_invalid_parallel "${parallel_payload}"
expect_invalid_memory_limit "${memory_payload}"
if [[ -e "${canary}" ]]; then
  printf '[FAIL] validation executed an injection payload\n' >&2
  rm -f "${canary}"
  failures=$((failures + 1))
else
  printf '[PASS] validation does not execute injection payloads\n'
fi

LOCAL_CI="${SCRIPT_DIR}/local-ci.sh"
ADMIN_CI="${SCRIPT_DIR}/admin-dashboard-go-ci.sh"
parallel_guard="validate_nilaway_parallel \"${dollar}{nilaway_parallel}\""
memory_guard="validate_nilaway_gomemlimit \"${dollar}{nilaway_gomemlimit}\""
if grep -Fq "${parallel_guard}" "${LOCAL_CI}" \
  && grep -Fq "${memory_guard}" "${LOCAL_CI}" \
  && grep -Fq "${memory_guard}" "${ADMIN_CI}"; then
  printf '[PASS] both CI entrypoints invoke the shared validators\n'
else
  printf '[FAIL] a CI entrypoint does not invoke the shared validator\n' >&2
  failures=$((failures + 1))
fi
if grep -Eq 'bash -c.*NILAWAY_(PARALLEL|GOMEMLIMIT)' "${LOCAL_CI}" "${ADMIN_CI}"; then
  printf '[FAIL] a NilAway input is interpolated into bash -c\n' >&2
  failures=$((failures + 1))
else
  printf '[PASS] NilAway inputs are not interpolated into bash -c\n'
fi

if (( failures > 0 )); then
  exit 1
fi
