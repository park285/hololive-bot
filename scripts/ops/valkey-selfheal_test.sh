#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_DIR="$(mktemp -d /tmp/valkey-selfheal-test.XXXXXX)"
trap 'rm -rf "${TMP_DIR}"' EXIT
grep -Fq 'COMPOSE_ENV_FILE="${COMPOSE_ENV_FILE:-/etc/stack-secrets/hololive-bot/compose.env}"' \
  "${ROOT_DIR}/scripts/ops/valkey-selfheal.sh"
grep -Fq 'COMPOSE_RUNNER="${REPO_DIR}/scripts/deploy/compose.sh"' \
  "${ROOT_DIR}/scripts/ops/valkey-selfheal.sh"
if grep -Fq 'docker compose' "${ROOT_DIR}/scripts/ops/valkey-selfheal.sh"; then
  echo "[FAIL] valkey self-heal bypasses the repository Compose owner" >&2
  exit 1
fi
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
  mkdir -p "${fixture}/scripts/ops/lib" "${fixture}/scripts/deploy"
  chmod go-w "${TMP_DIR}/${label}" "${fixture}" "${fixture}/scripts" "${fixture}/scripts/ops" "${fixture}/scripts/ops/lib" "${fixture}/scripts/deploy"
  cp "${ROOT_DIR}/scripts/ops/valkey-selfheal.sh" "${fixture}/scripts/ops/valkey-selfheal.sh"
  cp "${ROOT_DIR}/scripts/ops/lib/valkey-selfheal-state.sh" "${fixture}/scripts/ops/lib/valkey-selfheal-state.sh"
  cp "${ROOT_DIR}/scripts/ops/lib/valkey-selfheal-journal.sh" "${fixture}/scripts/ops/lib/valkey-selfheal-journal.sh"
  chmod +x "${fixture}/scripts/ops/valkey-selfheal.sh"
  cat >"${fixture}/scripts/deploy/compose.sh" <<'COMPOSE'
#!/usr/bin/env bash
set -euo pipefail
printf 'compose-wrapper %s\n' "$*" >>"${FAKE_DOCKER_LOG:?}"
if [ "${FAKE_DOCKER_FAIL_MUTATION:-0}" = "1" ]; then
  exit 42
fi
COMPOSE
  chmod +x "${fixture}/scripts/deploy/compose.sh"
  touch "${fixture}/docker-compose.prod.yml"
  printf 'CACHE_PASSWORD=test\n' >"${fixture}/compose.env"
  printf '%s\n' '{"version":2,"restart_count":0,"ping_failures":0,"epoch":0,"mutations":0,"next_eligible_at":0,"status":"monitoring","lock_token":""}' >"${fixture}/state"
  cp "${fixture}/state" "${fixture}/state.guard"
  cp "${fixture}/state" "${fixture}/state.receipt"
  : >"${fixture}/state.lock"
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
    if [[ " $* " == *' printenv CACHE_PASSWORD '* ]]; then
      printf '%s\n' "${FAKE_CONTAINER_SECRET_CANARY:-fixture-container-secret}"
      exit 0
    fi
    if [[ " $* " == *' REDISCLI_AUTH="$CACHE_PASSWORD" '* ]] && [ "${FAKE_DOCKER_PING_OK:-0}" = "1" ]; then
      printf 'PONG\n'
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
  local ping_ok="${9:-0}"
  local secret_canary="${10:-fixture-container-secret}"
  local now="${11:-123}"
  local state_file="${12:-${fixture}/state}"
  PATH="${fakebin}:${PATH}" \
    FAKE_DOCKER_LOG="${docker_log}" \
    FAKE_DOCKER_FAIL_MUTATION="${fail_mutation}" \
    FAKE_DOCKER_PING_OK="${ping_ok}" \
    FAKE_CONTAINER_SECRET_CANARY="${secret_canary}" \
    CACHE_PASSWORD="${secret_canary}" \
    REDISCLI_AUTH="${secret_canary}" \
    SELFHEAL_STATE="${state_file}" \
    SELFHEAL_JOURNAL="${fixture}/journal.jsonl" \
    SELFHEAL_NOW="${now}" \
    RECOVERY_COOLDOWN_SEC=60 \
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
  if grep -Eq '^(restart |compose-wrapper .* up )' "${docker_log}"; then
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
  if grep -Eq '^(restart |compose-wrapper .* up )' "${docker_log}"; then
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
  run_selfheal "${fixture}" "${fakebin}" "${docker_log}" --apply "${fixture}/compose.env" "${out_file}" "${err_file}" 1 0 fixture-container-secret 123 || true
  if run_selfheal "${fixture}" "${fakebin}" "${docker_log}" --apply "${fixture}/compose.env" "${out_file}" "${err_file}" 1 0 fixture-container-secret 183; then
    record_fail "expected exhausted recovery epoch to return non-zero"
    return
  fi
  recover_failed="$(grep -F '"event":"recover_failed"' "${err_file}" | tail -n 1 || true)"
  if [ -z "${recover_failed}" ]; then
    cat "${err_file}" >&2
    record_fail "expected recover_failed event"
    return
  fi
  if [[ "${recover_failed}" != *'"cmd":"COMPOSE_ENV_FILE='*'/scripts/deploy/compose.sh -f '* ]] ||
     [[ "${recover_failed}" != *'"terminal":"manual_intervention_required"'* ]]; then
    printf '%s\n' "${recover_failed}" >&2
    record_fail "recover_failed missing canonical compose runner detail"
    return
  fi
  if [[ "${recover_failed}" != *'"argv":["env","COMPOSE_ENV_FILE='*'","'*'/scripts/deploy/compose.sh","-f"'* ]]; then
    printf '%s\n' "${recover_failed}" >&2
    record_fail "recover_failed missing argv detail"
    return
  fi
  pass "failed recovery reports cmd and argv detail"
}

recovery_epoch_is_bounded_and_terminal() {
  local fixture fakebin docker_log out_file err_file mutation_count state
  fixture="$(setup_fixture bounded-epoch)"
  fakebin="${TMP_DIR}/bounded-epoch/bin"
  docker_log="${TMP_DIR}/bounded-epoch/docker.log"
  out_file="${TMP_DIR}/bounded-epoch/out.log"
  err_file="${TMP_DIR}/bounded-epoch/err.log"
  setup_fake_docker "${fakebin}" "${docker_log}"
  run_selfheal "${fixture}" "${fakebin}" "${docker_log}" --apply "${fixture}/compose.env" "${out_file}" "${err_file}" 1 0 fixture-container-secret 100 || true
  mutation_count="$(grep -Ec '^(restart |compose-wrapper .* up )' "${docker_log}" || true)"
  [ "${mutation_count}" -eq 1 ] || { record_fail "first recovery evaluation must reserve exactly one mutation"; return; }
  run_selfheal "${fixture}" "${fakebin}" "${docker_log}" --apply "${fixture}/compose.env" "${out_file}" "${err_file}" 1 0 fixture-container-secret 120 || true
  mutation_count="$(grep -Ec '^(restart |compose-wrapper .* up )' "${docker_log}" || true)"
  [ "${mutation_count}" -eq 1 ] || { record_fail "cooldown must prevent a second mutation"; return; }

  run_selfheal "${fixture}" "${fakebin}" "${docker_log}" --apply "${fixture}/compose.env" "${out_file}" "${err_file}" 1 0 fixture-container-secret 160 || true
  run_selfheal "${fixture}" "${fakebin}" "${docker_log}" --apply "${fixture}/compose.env" "${out_file}" "${err_file}" 1 0 fixture-container-secret 220 || true
  mutation_count="$(grep -Ec '^(restart |compose-wrapper .* up )' "${docker_log}" || true)"
  state="$(cat "${fixture}/state")"
  if [ "${mutation_count}" -ne 2 ] ||
     ! jq -e '.version == 2 and .mutations == 2 and .next_eligible_at == 0 and .status == "manual_intervention_required" and (.lock_token | length > 0)' <<<"${state}" >/dev/null; then
    printf 'mutations=%s state=%s\n' "${mutation_count}" "${state}" >&2
    record_fail "one recovery epoch must stop permanently after two mutations"
    return
  fi
  if find "${fixture}" -maxdepth 1 \( -name 'state.tmp' -o -name 'state.guard.tmp' -o -name 'state.receipt.tmp' \) -print -quit | grep -q .; then
    record_fail "atomic state commit left a temporary file"
    return
  fi
  pass "recovery epoch enforces cooldown and terminal two-mutation budget"
}

state_persist_failure_prevents_mutation() {
  local fixture fakebin docker_log out_file err_file probe_status=0
  fixture="$(setup_fixture state-failure)"
  fakebin="${TMP_DIR}/state-failure/bin"
  docker_log="${TMP_DIR}/state-failure/docker.log"
  out_file="${TMP_DIR}/state-failure/out.log"
  err_file="${TMP_DIR}/state-failure/err.log"
  setup_fake_docker "${fakebin}" "${docker_log}"
  : >"${fixture}/journal.jsonl"
  chmod 600 "${fixture}/state" "${fixture}/state.lock" "${fixture}/journal.jsonl"
  chmod u-w "${fixture}"

  run_selfheal "${fixture}" "${fakebin}" "${docker_log}" --apply "${fixture}/compose.env" "${out_file}" "${err_file}" 1 0 fixture-container-secret 100 || probe_status="$?"
  chmod u+w "${fixture}"
  if grep -Eq '^(restart |compose-wrapper .* up )' "${docker_log}"; then
    record_fail "state persistence failure must prevent all recovery mutations"
    return
  fi
  if [ "${probe_status}" -eq 0 ]; then
    record_fail "state persistence boundary failure must return non-zero"
    return
  fi
  pass "state persistence failure causes zero recovery mutation"
}

healthy_probe_resets_terminal_epoch() {
  local fixture fakebin docker_log out_file err_file
  fixture="$(setup_fixture healthy-reset)"
  fakebin="${TMP_DIR}/healthy-reset/bin"
  docker_log="${TMP_DIR}/healthy-reset/docker.log"
  out_file="${TMP_DIR}/healthy-reset/out.log"
  err_file="${TMP_DIR}/healthy-reset/err.log"
  setup_fake_docker "${fakebin}" "${docker_log}"
  printf '{"version":2,"restart_count":9,"ping_failures":8,"epoch":100,"mutations":2,"next_eligible_at":0,"status":"manual_intervention_required","lock_token":"%s"}\n' \
    "$(stat -Lc '%f:%d:%i' "${fixture}/state.lock")" >"${fixture}/state"
  cp "${fixture}/state" "${fixture}/state.guard"
  cp "${fixture}/state" "${fixture}/state.receipt"
  run_selfheal "${fixture}" "${fakebin}" "${docker_log}" --apply "${fixture}/compose.env" "${out_file}" "${err_file}" 0 1 fixture-container-secret 300
  if ! jq -e '.version == 2 and .restart_count == 0 and .ping_failures == 0 and .epoch == 0 and .mutations == 0 and .next_eligible_at == 0 and .status == "monitoring" and .lock_token == ""' "${fixture}/state" >/dev/null; then
    cat "${fixture}/state" >&2
    record_fail "healthy probe must reset the recovery epoch"
    return
  fi
  pass "healthy probe atomically resets terminal recovery state"
}

keeps_cache_secret_inside_container() {
  local fixture fakebin docker_log out_file err_file secret_canary
  fixture="$(setup_fixture secret-boundary)"
  fakebin="${TMP_DIR}/secret-boundary/bin"
  docker_log="${TMP_DIR}/secret-boundary/docker.log"
  out_file="${TMP_DIR}/secret-boundary/out.log"
  err_file="${TMP_DIR}/secret-boundary/err.log"
  secret_canary='host-secret-canary-must-not-appear'
  setup_fake_docker "${fakebin}" "${docker_log}"

  if ! run_selfheal "${fixture}" "${fakebin}" "${docker_log}" --apply "${fixture}/compose.env" "${out_file}" "${err_file}" 0 1 "${secret_canary}"; then
    cat "${err_file}" >&2
    record_fail "container-owned PING should succeed"
    return
  fi

  if grep -R -Fq "${secret_canary}" "${docker_log}" "${out_file}" "${err_file}" "${fixture}/journal.jsonl" 2>/dev/null; then
    record_fail "cache secret canary reached host argv, log, or evidence"
    return
  fi
  if grep -Fq 'printenv CACHE_PASSWORD' "${docker_log}" || grep -Fq -- '-e REDISCLI_AUTH=' "${docker_log}"; then
    cat "${docker_log}" >&2
    record_fail "host extracted or forwarded the cache credential"
    return
  fi
  if ! grep -Fq "REDISCLI_AUTH=\"\$CACHE_PASSWORD\"" "${docker_log}"; then
    cat "${docker_log}" >&2
    record_fail "PING did not retain container-owned credential expansion"
    return
  fi

  pass "cache credential stays inside the container"
}

rejects_injected_env_file_without_executing_payload
dry_run_performs_zero_docker_mutations
refuses_world_writable_env_file_before_recovery
failed_recovery_reports_failed_command_detail
keeps_cache_secret_inside_container
recovery_epoch_is_bounded_and_terminal
state_persist_failure_prevents_mutation
healthy_probe_resets_terminal_epoch
bash "${ROOT_DIR}/scripts/ops/valkey-selfheal_state_contract_test.sh"
bash "${ROOT_DIR}/scripts/ops/valkey-selfheal_journal_contract_test.sh"

if (( failures > 0 )); then
  echo "[FAIL] valkey self-heal tests failed: ${failures}" >&2
  exit 1
fi

echo "ok: valkey self-heal tests passed"
