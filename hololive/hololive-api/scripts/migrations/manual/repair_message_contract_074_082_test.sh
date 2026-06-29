#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPAIR_SCRIPT="${SCRIPT_DIR}/repair_message_contract_074_082.sh"
TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "${TMP_ROOT}"' EXIT

LAST_STATUS=0
LAST_OUTPUT=""
PASSED=0

write_fake_psql() {
  local dir="$1"
  mkdir -p "${dir}"
  cat >"${dir}/psql" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

case "${FAKE_PSQL_MODE:-audit_clean}" in
  audit_fail)
    echo "psql: simulated audit failure" >&2
    exit 2
    ;;
  audit_clean)
    printf '%s\n' '074_create_message_strings.sql|present|ledger|OK'
    ;;
  audit_damaged)
    printf '%s\n' '074_create_message_strings.sql|missing|ledger|DAMAGED'
    ;;
  *)
    echo "unknown FAKE_PSQL_MODE=${FAKE_PSQL_MODE:-}" >&2
    exit 99
    ;;
esac
EOF
  chmod +x "${dir}/psql"
}

run_repair() {
  local mode="$1"
  local fake_bin="${TMP_ROOT}/bin-${mode}"
  write_fake_psql "${fake_bin}"

  set +e
  LAST_OUTPUT="$(
    PATH="${fake_bin}:${PATH}" \
      PGPASSWORD=fixture \
      PGHOST=fixture-host \
      PGPORT=5432 \
      PGUSER=fixture-user \
      PGDATABASE=fixture-db \
      FAKE_PSQL_MODE="${mode}" \
      MIGRATION_REPAIR_APPLY=0 \
      "${REPAIR_SCRIPT}" 2>&1
  )"
  LAST_STATUS=$?
  set -e
}

assert_status() {
  local name="$1"
  local want="$2"
  if [[ "${LAST_STATUS}" -ne "${want}" ]]; then
    printf 'not ok - %s want status %s got %s\n%s\n' "${name}" "${want}" "${LAST_STATUS}" "${LAST_OUTPUT}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - %s\n' "${name}"
}

assert_nonzero() {
  local name="$1"
  if [[ "${LAST_STATUS}" -eq 0 ]]; then
    printf 'not ok - %s unexpectedly exited 0\n%s\n' "${name}" "${LAST_OUTPUT}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - %s\n' "${name}"
}

assert_contains() {
  local name="$1"
  local needle="$2"
  if [[ "${LAST_OUTPUT}" != *"${needle}"* ]]; then
    printf 'not ok - %s missing %q\n%s\n' "${name}" "${needle}" "${LAST_OUTPUT}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - %s\n' "${name}"
}

assert_not_contains() {
  local name="$1"
  local needle="$2"
  if [[ "${LAST_OUTPUT}" == *"${needle}"* ]]; then
    printf 'not ok - %s unexpectedly contained %q\n%s\n' "${name}" "${needle}" "${LAST_OUTPUT}" >&2
    exit 1
  fi
  PASSED=$((PASSED + 1))
  printf 'ok - %s\n' "${name}"
}

case_audit_failure_is_not_clean() {
  run_repair audit_fail
  assert_nonzero "audit failure exits nonzero"
  assert_contains "audit failure explains refusal" "audit query failed"
  assert_contains "audit failure preserves psql stderr" "psql: simulated audit failure"
  assert_not_contains "audit failure is not treated as clean" "nothing to repair"
}

case_clean_audit_noops() {
  run_repair audit_clean
  assert_status "clean audit exits 0" 0
  assert_contains "clean audit reports no repair" "nothing to repair"
}

case_damaged_audit_dry_runs() {
  run_repair audit_damaged
  assert_status "damaged audit dry-run exits 0" 0
  assert_contains "damaged audit is listed" "DAMAGED:074_create_message_strings.sql"
  assert_contains "damaged audit plans repair" "[dry-run] would re-run 074_create_message_strings.sql"
}

case_audit_failure_is_not_clean
case_clean_audit_noops
case_damaged_audit_dry_runs

printf 'ok - repair_message_contract_074_082_test passed (%s assertions)\n' "${PASSED}"
