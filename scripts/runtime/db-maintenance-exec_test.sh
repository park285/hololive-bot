#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TEST_TMP_DIR="$(mktemp -d)"
trap 'rm -rf -- "${TEST_TMP_DIR}"' EXIT

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

mkdir -p \
  "${TEST_TMP_DIR}/bin" \
  "${TEST_TMP_DIR}/migrations" \
  "${TEST_TMP_DIR}/secrets/postgres" \
  "${TEST_TMP_DIR}/secrets/certs" \
  "${TEST_TMP_DIR}/output"
: >"${TEST_TMP_DIR}/secrets/postgres/pg_service.conf"
: >"${TEST_TMP_DIR}/secrets/postgres/pgpass"
: >"${TEST_TMP_DIR}/secrets/certs/postgres-ca.pem"

cat >"${TEST_TMP_DIR}/bin/sudo" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "${1:-}" == "-n" ]] && shift
exec "$@"
EOF

cat >"${TEST_TMP_DIR}/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in
  inspect)
    if [[ "$*" == *'.Config.Image'* ]]; then
      printf '%s\n' 'postgres:test'
    else
      printf '%s\n' 'test-network'
    fi
    ;;
  run)
    printf '%s\n' "$*" >"${MOCK_DOCKER_LOG}"
    ;;
  *)
    exit 1
    ;;
esac
EOF
chmod +x "${TEST_TMP_DIR}/bin/sudo" "${TEST_TMP_DIR}/bin/docker"

export PATH="${TEST_TMP_DIR}/bin:${PATH}"
export MIGRATIONS_DIR="${TEST_TMP_DIR}/migrations"
export SECRETS_DIR="${TEST_TMP_DIR}/secrets"
export MOCK_DOCKER_LOG="${TEST_TMP_DIR}/docker.log"

if DB_MAINTENANCE_OUTPUT_FILE=relative.sql \
  "${ROOT_DIR}/scripts/runtime/db-maintenance-exec.sh" true >"${TEST_TMP_DIR}/relative.out" 2>&1; then
  fail "relative rollback output path was accepted"
fi
grep -Fq 'must be an absolute path' "${TEST_TMP_DIR}/relative.out" \
  || fail "relative path rejection was not explicit"

existing="${TEST_TMP_DIR}/output/existing.sql"
: >"${existing}"
if DB_MAINTENANCE_OUTPUT_FILE="${existing}" \
  "${ROOT_DIR}/scripts/runtime/db-maintenance-exec.sh" true >"${TEST_TMP_DIR}/existing.out" 2>&1; then
  fail "existing rollback output file was accepted"
fi
grep -Fq 'must be a new file' "${TEST_TMP_DIR}/existing.out" \
  || fail "existing path rejection was not explicit"

output="${TEST_TMP_DIR}/output/rollback.sql"
DB_MAINTENANCE_OUTPUT_FILE="${output}" \
  "${ROOT_DIR}/scripts/runtime/db-maintenance-exec.sh" true

[[ -f "${output}" ]] || fail "rollback output file was not created"
[[ "$(stat -c '%a' -- "${output}")" == "600" ]] || fail "rollback output file mode is not 0600"
grep -Fq -- "-v ${output}:/maintenance-output/rollback.sql:rw" "${MOCK_DOCKER_LOG}" \
  || fail "rollback output was not mounted as the exact precreated file"

echo "[PASS] db maintenance rollback output is explicit, new, mode 0600, and file-mounted"
