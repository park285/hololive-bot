#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_DIR="$(mktemp -d /tmp/valkey-selfheal-test.XXXXXX)"
trap 'rm -rf "${TMP_DIR}"' EXIT

failures=0

record_fail() {
  echo "[FAIL] $*" >&2
  failures=$((failures + 1))
}

pass() {
  echo "[PASS] $*"
}

setup_fixture() {
  local label="$1"
  local fixture="${TMP_DIR}/${label}/repo"

  mkdir -p "${fixture}/scripts/ops"
  chmod go-w "${TMP_DIR}/${label}" "${fixture}" "${fixture}/scripts" "${fixture}/scripts/ops"
  cp "${ROOT_DIR}/scripts/ops/valkey-selfheal.sh" "${fixture}/scripts/ops/valkey-selfheal.sh"
  chmod +x "${fixture}/scripts/ops/valkey-selfheal.sh"
  touch "${fixture}/docker-compose.prod.yml"
  printf 'CACHE_PASSWORD=test\n' >"${fixture}/compose.env"
  chmod 600 "${fixture}/compose.env"

  printf '%s\n' "${fixture}"
}

setup_fake_docker() {
  local fakebin="$1"
  local log_file="$2"

  mkdir -p "${fakebin}"
  cat >"${fakebin}/docker" <<'EOF'
#!/usr/bin/env bash
set -u

printf '%s\n' "$*" >>"${FAKE_DOCKER_LOG:?}"

case "${1:-}" in
  inspect)
    printf '%s\n' "${FAKE_DOCKER_RESTART_COUNT:-0}"
    ;;
  exec)
    if [ "${3:-}" = "printenv" ]; then
      printf 'test\n'
      exit 0
    fi
    exit 1
    ;;
  restart)
    if [ "${FAKE_DOCKER_FAIL_MUTATION:-0}" = "1" ]; then
      exit 42
    fi
    exit 0
    ;;
  compose)
    if [ "${FAKE_DOCKER_FAIL_MUTATION:-0}" = "1" ]; then
      exit 42
    fi
    exit 0
    ;;
  ps)
    exit 1
    ;;
esac
EOF
  chmod +x "${fakebin}/docker"
  : >"${log_file}"
}

run_selfheal() {
  local fixture="$1"
  local fakebin="$2"
  local docker_log="$3"
  local mode="$4"
  local env_file="$5"
  local out_file="$6"
  local err_file="$7"
  local fail_mutation="${8:-0}"

  PATH="${fakebin}:${PATH}" \
    FAKE_DOCKER_LOG="${docker_log}" \
    FAKE_DOCKER_FAIL_MUTATION="${fail_mutation}" \
    SELFHEAL_STATE="${fixture}/state" \
    SELFHEAL_JOURNAL="${fixture}/journal.jsonl" \
    SELFHEAL_NOW=123 \
    RESTART_SETTLE_SEC=0 \
    CRASH_RESTART_DELTA=999 \
    PING_FAIL_THRESHOLD=1 \
    REPO_DIR="${fixture}" \
    COMPOSE_FILE="${fixture}/docker-compose.prod.yml" \
    COMPOSE_ENV_FILE="${env_file}" \
    "${fixture}/scripts/ops/valkey-selfheal.sh" "${mode}" >"${out_file}" 2>"${err_file}"
}

expect_validation_failure() {
  local label="$1"
  local err_file="$2"
  local recover_failed

  if ! grep -Fq "input_validation_failed" "${err_file}"; then
    cat "${err_file}" >&2
    record_fail "expected input validation failure: ${label}"
    return
  fi
  recover_failed="$(grep -F '"event":"recover_failed"' "${err_file}" | tail -n 1 || true)"
  if [[ "${recover_failed}" != *'"cmd":'* ]] || [[ "${recover_failed}" != *'"argv":['* ]]; then
    printf '%s\n' "${recover_failed}" >&2
    record_fail "recover_failed missing cmd or argv detail: ${label}"
    return
  fi
  pass "${label}"
}

rejects_injected_env_file_without_executing_payload() {
  local fixture fakebin docker_log out_file err_file payload_file poisoned_env
  fixture="$(setup_fixture injection)"
  fakebin="${TMP_DIR}/injection/bin"
  docker_log="${TMP_DIR}/injection/docker.log"
  out_file="${TMP_DIR}/injection/out.log"
  err_file="${TMP_DIR}/injection/err.log"
  payload_file="${TMP_DIR}/injection/pwned"
  poisoned_env="/tmp/x; touch ${payload_file}; #"
  setup_fake_docker "${fakebin}" "${docker_log}"

  if run_selfheal "${fixture}" "${fakebin}" "${docker_log}" --apply "${poisoned_env}" "${out_file}" "${err_file}"; then
    record_fail "expected poisoned COMPOSE_ENV_FILE to be rejected"
  fi

  if [ -e "${payload_file}" ]; then
    cat "${err_file}" >&2
    record_fail "injection payload executed"
    return
  fi

  expect_validation_failure "poisoned COMPOSE_ENV_FILE is rejected without payload execution" "${err_file}"
}

dry_run_performs_zero_docker_mutations() {
  local fixture fakebin docker_log out_file err_file
  fixture="$(setup_fixture dry-run)"
  fakebin="${TMP_DIR}/dry-run/bin"
  docker_log="${TMP_DIR}/dry-run/docker.log"
  out_file="${TMP_DIR}/dry-run/out.log"
  err_file="${TMP_DIR}/dry-run/err.log"
  setup_fake_docker "${fakebin}" "${docker_log}"

  run_selfheal "${fixture}" "${fakebin}" "${docker_log}" --dry-run "${fixture}/compose.env" "${out_file}" "${err_file}" || true

  if grep -Eq '^(restart |compose .* up )' "${docker_log}"; then
    cat "${docker_log}" >&2
    record_fail "dry-run performed docker mutation"
    return
  fi
  if grep -Eq '^exec ' "${docker_log}"; then
    cat "${docker_log}" >&2
    record_fail "dry-run performed docker exec"
    return
  fi
  pass "dry-run performs zero docker mutations"
}

refuses_world_writable_env_file_before_recovery() {
  local fixture fakebin docker_log out_file err_file
  fixture="$(setup_fixture world-writable)"
  fakebin="${TMP_DIR}/world-writable/bin"
  docker_log="${TMP_DIR}/world-writable/docker.log"
  out_file="${TMP_DIR}/world-writable/out.log"
  err_file="${TMP_DIR}/world-writable/err.log"
  chmod 666 "${fixture}/compose.env"
  setup_fake_docker "${fakebin}" "${docker_log}"

  if run_selfheal "${fixture}" "${fakebin}" "${docker_log}" --apply "${fixture}/compose.env" "${out_file}" "${err_file}"; then
    record_fail "expected world-writable COMPOSE_ENV_FILE to be rejected"
  fi

  if grep -Eq '^(restart |compose .* up )' "${docker_log}"; then
    cat "${docker_log}" >&2
    record_fail "world-writable COMPOSE_ENV_FILE reached docker mutation"
    return
  fi

  expect_validation_failure "world-writable COMPOSE_ENV_FILE is refused" "${err_file}"
}

failed_recovery_reports_failed_command_detail() {
  local fixture fakebin docker_log out_file err_file recover_failed
  fixture="$(setup_fixture failed-recovery)"
  fakebin="${TMP_DIR}/failed-recovery/bin"
  docker_log="${TMP_DIR}/failed-recovery/docker.log"
  out_file="${TMP_DIR}/failed-recovery/out.log"
  err_file="${TMP_DIR}/failed-recovery/err.log"
  setup_fake_docker "${fakebin}" "${docker_log}"

  if run_selfheal "${fixture}" "${fakebin}" "${docker_log}" --apply "${fixture}/compose.env" "${out_file}" "${err_file}" 1; then
    record_fail "expected failed recovery to return non-zero"
    return
  fi

  recover_failed="$(grep -F '"event":"recover_failed"' "${err_file}" | tail -n 1 || true)"
  if [ -z "${recover_failed}" ]; then
    cat "${err_file}" >&2
    record_fail "expected recover_failed event"
    return
  fi
  if [[ "${recover_failed}" != *'"cmd":"docker compose --env-file '* ]]; then
    printf '%s\n' "${recover_failed}" >&2
    record_fail "recover_failed missing legacy cmd detail"
    return
  fi
  if [[ "${recover_failed}" != *'"argv":["docker","compose","--env-file"'* ]]; then
    printf '%s\n' "${recover_failed}" >&2
    record_fail "recover_failed missing argv detail"
    return
  fi

  pass "failed recovery reports cmd and argv detail"
}

rejects_injected_env_file_without_executing_payload
dry_run_performs_zero_docker_mutations
refuses_world_writable_env_file_before_recovery
failed_recovery_reports_failed_command_detail

if (( failures > 0 )); then
  echo "[FAIL] valkey self-heal tests failed: ${failures}" >&2
  exit 1
fi

echo "ok: valkey self-heal tests passed"
