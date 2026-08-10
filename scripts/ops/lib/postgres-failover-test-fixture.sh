#!/usr/bin/env bash
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  printf '%s\n' "source-only helper: ${BASH_SOURCE[0]}" >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TMP_DIR="$(mktemp -d /tmp/postgres-failover-test.XXXXXX)"
SYSTEM_CREDENTIAL_TEST_ROOT=""
cleanup_postgres_failover_fixture() {
  rm -rf "${TMP_DIR}"
  if [[ -n "${SYSTEM_CREDENTIAL_TEST_ROOT}" ]]; then
    case "${SYSTEM_CREDENTIAL_TEST_ROOT}" in
      /run/credentials/postgres-failover-test.*) rm -rf -- "${SYSTEM_CREDENTIAL_TEST_ROOT}" ;;
      *) printf 'refusing unexpected credential fixture cleanup: %s\n' "${SYSTEM_CREDENTIAL_TEST_ROOT}" >&2 ;;
    esac
  fi
}
trap cleanup_postgres_failover_fixture EXIT
EXEC_ROOT="${TMP_DIR}/exec"; mkdir -p "${EXEC_ROOT}"
(cd "${ROOT_DIR}" && cp --parents scripts/ops/postgres-failover.sh scripts/ops/lib/postgres-failover-lib.sh scripts/ops/lib/postgres-failover-transition-lib.sh "${EXEC_ROOT}")
chmod -R go-w "${EXEC_ROOT}"
CONTROLLER="${EXEC_ROOT}/scripts/ops/postgres-failover.sh"
if [[ "$(id -u)" == "0" ]]; then CONTROLLER_TEST_MODE=0; else CONTROLLER_TEST_MODE=1; fi
failures=0
pass() { printf '[PASS] %s\n' "$*"; }
fail() { printf '[FAIL] %s\n' "$*" >&2; failures=$((failures + 1)); }
finish_postgres_failover_tests() {
  if (( failures > 0 )); then
    printf '[FAIL] postgres failover tests failed: %s\n' "${failures}" >&2
    exit 1
  fi
  printf 'ok: postgres failover tests passed\n'
}
setup_fake_psql() {
  local dir="$1"
  mkdir -p "${dir}"
  cat >"${dir}/psql" <<'FAKE_PSQL'
#!/usr/bin/env bash
set -u
printf '%s\n' "$*" >>"${FAKE_PSQL_LOG:?}"
if [[ -n "${FAKE_PGPASS_METADATA_LOG:-}" ]]; then
  stat -c 'mode=%a owner=%u group=%g path=%n' -- "${PGPASSFILE:?}" >>"${FAKE_PGPASS_METADATA_LOG}"
fi
if [[ "${FAKE_REQUIRE_PRIVATE_PGPASS:-0}" == "1" && "$(stat -c '%a' -- "${PGPASSFILE:?}")" != "600" ]]; then
  exit 3
fi
args="$*"
if [[ "${args}" == *"pg_promote"* ]]; then
  case "${FAKE_PROMOTE_RESULT:-success}" in
    success) : >"${FAKE_PROMOTED_FILE:?}"; printf 't\n'; exit 0 ;;
    fail_primary) : >"${FAKE_PROMOTED_FILE:?}"; exit 1 ;;
    fail_standby) exit 1 ;;
    *) exit 2 ;;
  esac
fi
if [[ " ${args} " == *" -h ${FAKE_PRIMARY_HOST:-100.100.1.8} "* ]]; then
  count=0
  [[ -r "${FAKE_PRIMARY_COUNT_FILE:?}" ]] && count="$(cat "${FAKE_PRIMARY_COUNT_FILE}")"
  count=$((count + 1))
  printf '%s\n' "${count}" >"${FAKE_PRIMARY_COUNT_FILE}"
  IFS=',' read -r -a sequence <<<"${FAKE_PRIMARY_SEQUENCE:-up}"
  index=$((count - 1))
  if (( index >= ${#sequence[@]} )); then index=$((${#sequence[@]} - 1)); fi
  case "${sequence[$index]}" in
    up) printf '%s\n' "${FAKE_PRIMARY_STATUS:-f|0/20|off}"; exit 0 ;;
    readonly) printf 'f|0/20|on\n'; exit 0 ;;
    standby) printf 't|0/20|on\n'; exit 0 ;;
    down) exit 1 ;;
    *) exit 2 ;;
  esac
fi
if [[ "${args}" == *"SELECT pg_is_in_recovery()"* || "${args}" == *"WITH role AS"* ]]; then
  if [[ -e "${FAKE_PROMOTED_FILE:?}" ]]; then
    printf 'f|0/20|0/20|f|off\n'
  else
    printf '%s\n' "${FAKE_LOCAL_STATUS:-t|0/20|0/20|f|on}"
  fi
  exit 0
fi
exit 2
FAKE_PSQL
  chmod 0755 "${dir}/psql"
}
setup_case() {
  local label="$1"
  local root="${TMP_DIR}/${label}"
  mkdir -p "${root}/state" "${root}/bin" "${root}/hooks"
  chmod 0700 "${root}/state" "${root}/hooks"
  setup_fake_psql "${root}/bin"
  : >"${root}/psql.log"
  : >"${root}/pgpass-metadata.log"
  : >"${root}/hooks.log"
  : >"${root}/primary.count"
  rm -f "${root}/primary.count" "${root}/promoted" "${root}/state/health.signal"
  printf '100.100.1.8:5433:hololive:hololive_replicator:test\n100.100.1.5:5434:hololive:hololive_replicator:test\n' >"${root}/pgpass"
  printf 'test-ca\n' >"${root}/postgres-ca.pem"
  chmod 0600 "${root}/pgpass"
  chmod 0644 "${root}/postgres-ca.pem"
  cat >"${root}/hooks/fence.sh" <<'FENCE'
#!/usr/bin/env bash
printf 'fence\n' >>"${FAKE_HOOK_LOG:?}"
printf '%s\n' "${FAKE_FENCE_OUTPUT:-FENCED|${POSTGRES_FAILOVER_PRIMARY_HOST}|${POSTGRES_FAILOVER_NEW_PRIMARY_HOST}:${POSTGRES_FAILOVER_NEW_PRIMARY_PORT}|${POSTGRES_FAILOVER_REQUEST_ID}|${FAKE_FENCE_TOKEN:-${POSTGRES_FAILOVER_REQUEST_ID}}}"
FENCE
  cat >"${root}/hooks/route.sh" <<'ROUTE'
#!/usr/bin/env bash
printf 'route\n' >>"${FAKE_HOOK_LOG:?}"
printf '%s\n' "${FAKE_ROUTE_OUTPUT:-ROUTED|${POSTGRES_FAILOVER_NEW_PRIMARY_HOST}:${POSTGRES_FAILOVER_NEW_PRIMARY_PORT}|${POSTGRES_FAILOVER_FENCE_TOKEN}}"
ROUTE
  chmod 0600 "${root}/hooks/fence.sh" "${root}/hooks/route.sh"
  printf '%s\n' "${root}"
}
seed_ready_state() {
  local root="$1"
  local failure_count="${2:-1}"
  local first_failure="${3:-100}"
  local last_healthy="${4:-90}"
  local primary_lsn="${5:-0/20}"
  local replay_lsn="${6:-0/20}"
  printf '1\t%s\t%s\t%s\t%s\t%s\tmonitoring\t-\n' \
    "${failure_count}" "${first_failure}" "${last_healthy}" "${primary_lsn}" "${replay_lsn}" >"${root}/state/state.tsv"
  chmod 0600 "${root}/state/state.tsv"
}
run_controller() {
  local root="$1" mode="$2" sequence="$3"
  shift 3
  env \
    PATH="${root}/bin:${PATH}" \
    FAKE_PSQL_LOG="${root}/psql.log" \
    FAKE_PGPASS_METADATA_LOG="${root}/pgpass-metadata.log" \
    FAKE_HOOK_LOG="${root}/hooks.log" \
    FAKE_PROMOTED_FILE="${root}/promoted" \
    FAKE_PRIMARY_COUNT_FILE="${root}/primary.count" \
    FAKE_PRIMARY_SEQUENCE="${sequence}" \
    POSTGRES_FAILOVER_ALLOW_NON_ROOT_FOR_TEST="${CONTROLLER_TEST_MODE}" \
    POSTGRES_FAILOVER_SERVICE_USER="$(id -un)" \
    POSTGRES_FAILOVER_PSQL_PATH="${root}/bin/psql" \
    POSTGRES_FAILOVER_PGPASS_FILE="${root}/pgpass" \
    POSTGRES_FAILOVER_CA_FILE="${root}/postgres-ca.pem" \
    POSTGRES_FAILOVER_NOW=150 \
    POSTGRES_FAILOVER_STATE_DIR="${root}/state" \
    POSTGRES_FAILOVER_FAILURE_THRESHOLD=2 \
    POSTGRES_FAILOVER_MIN_OUTAGE_SEC=30 \
    POSTGRES_FAILOVER_MAX_LAST_HEALTHY_AGE_SEC=120 \
    POSTGRES_FAILOVER_MAX_KNOWN_LAG_BYTES=0 \
    POSTGRES_FAILOVER_PRIMARY_HOST=100.100.1.8 \
    POSTGRES_FAILOVER_NEW_PRIMARY_HOST=100.100.1.5 \
    POSTGRES_FAILOVER_FENCE_COMMAND="${root}/hooks/fence.sh" \
    POSTGRES_FAILOVER_ROUTE_COMMAND="${root}/hooks/route.sh" \
    "$@" \
    /usr/bin/env bash "${CONTROLLER}" "${mode}" >"${root}/out.log" 2>"${root}/err.log"
}
