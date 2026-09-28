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

# rollback artifact 출력 마운트(DB_MAINTENANCE_OUTPUT_FILE)는 유일한 사용처였던 preflight-114-restore.sh와 함께
# 지웠다(DEC-20260926-hololive-retired-rollback-tooling, stack-audit 2026-09-26 T19). 남은 계약은 읽기 전용 마운트뿐이다.
DB_MAINTENANCE_OUTPUT_FILE="${TEST_TMP_DIR}/output/rollback.sql" \
  "${ROOT_DIR}/scripts/runtime/db-maintenance-exec.sh" true

[[ ! -e "${TEST_TMP_DIR}/output/rollback.sql" ]] || fail "retired rollback output file was created"
grep -Fq -- "-v ${MIGRATIONS_DIR}:/migrations:ro" "${MOCK_DOCKER_LOG}" \
  || fail "migrations were not mounted read-only"
grep -Fq -- "-v ${SECRETS_DIR}/postgres:/run/hololive-bot/postgres:ro" "${MOCK_DOCKER_LOG}" \
  || fail "postgres service files were not mounted read-only"
if grep -Fq -- "/maintenance-output" "${MOCK_DOCKER_LOG}"; then
  fail "retired rollback output mount is still present"
fi
if grep -Eq -- '-v [^ ]+:rw' "${MOCK_DOCKER_LOG}"; then
  fail "maintenance container received a writable host mount"
fi

echo "[PASS] db maintenance container mounts only read-only inputs"
