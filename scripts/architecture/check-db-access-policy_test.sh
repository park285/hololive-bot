#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHECKER="${ROOT_DIR}/scripts/architecture/check-db-access-policy.sh"
TEST_TMP_DIR="$(mktemp -d)"
trap 'rm -rf -- "${TEST_TMP_DIR}"' EXIT

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

fixture="${TEST_TMP_DIR}/fixture"
mkdir -p "${fixture}/hololive" "${fixture}/admin-dashboard" "${fixture}/scripts" "${fixture}/internal"
: >"${fixture}/go.mod"

ROOT_DIR="${fixture}" "${CHECKER}" \
  >"${TEST_TMP_DIR}/clean.out" 2>&1 \
  || fail "clean fixture was rejected"

cat >"${fixture}/internal/probe.go" <<'EOF'
package internal

func forbidden() { gorm.Open() }
EOF
if ROOT_DIR="${fixture}" "${CHECKER}" \
  >"${TEST_TMP_DIR}/internal.out" 2>&1; then
  fail "disallowed DB token under internal/ was not detected"
fi
grep -Fq 'internal/probe.go' "${TEST_TMP_DIR}/internal.out" \
  || fail "internal/ violation did not identify its source"
rm -- "${fixture}/internal/probe.go"

cat >"${fixture}/root_probe.go" <<'EOF'
package fixture

const forbidden = "entgo.io/ent"
EOF
if ROOT_DIR="${fixture}" "${CHECKER}" \
  >"${TEST_TMP_DIR}/root.out" 2>&1; then
  fail "disallowed DB token in a root Go file was not detected"
fi
grep -Fq 'root_probe.go' "${TEST_TMP_DIR}/root.out" \
  || fail "root Go violation did not identify its source"

echo "[PASS] DB access policy scans internal/ and root Go files"
