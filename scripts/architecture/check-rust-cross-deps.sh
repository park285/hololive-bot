#!/usr/bin/env bash
# Rust 교차 의존 검사: alarm <-> scraper <-> dispatcher 간 교차 의존 금지
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
RUST_DIR="${ROOT_DIR}/hololive/hololive-rs"
FAIL=0

check_no_dep() {
  local from="$1" forbidden="$2"
  echo "  Checking ${from} does not depend on ${forbidden}..."
  local tree_output
  if ! tree_output=$(cargo tree -p "${from}" --manifest-path "${RUST_DIR}/Cargo.toml" 2>&1); then
    echo "  FAIL: cargo tree -p ${from} failed:"
    echo "${tree_output}" | head -5
    FAIL=1
    return
  fi
  if echo "${tree_output}" | grep -q " ${forbidden}"; then
    echo "  FAIL: ${from} depends on ${forbidden}"
    FAIL=1
  else
    echo "  OK"
  fi
}

echo "[Rust] Cross-dependency check"

# alarm <-> scraper 교차 의존 금지
check_no_dep "alarm-app" "scraper"
check_no_dep "scraper-app" "alarm"

# dispatcher -> scraper 교차 의존 금지
check_no_dep "dispatcher-app" "scraper"

if [ "$FAIL" -ne 0 ]; then
  echo "[Rust] Cross-dependency check FAILED"
  exit 1
fi

echo "[Rust] Cross-dependency check passed"
