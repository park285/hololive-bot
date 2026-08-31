#!/usr/bin/env bash
# valkey-selfheal.sh — valkey-cache crash loop 능동 자가복구 watchdog (1회 평가)
#
# 호출 전제: systemd timer 등으로 주기 실행(예: 30s). 상태는 STATE_FILE 에 persist.
# 기본 --dry-run(감지/저널만, 복구 미실행). 실제 복구는 --apply.
#
# 복구 단계(능동):
#   한 recovery epoch에서 restart 1회, cooldown, recreate 1회까지만 수행한다.
#   두 번째 실패 뒤에는 정상 PING이 확인될 때까지 manual_intervention_required를 유지한다.

set -uo pipefail
unset CACHE_PASSWORD REDISCLI_AUTH

VALKEY_CONTAINER="${VALKEY_CONTAINER:-valkey-cache}"
STATE_FILE="${SELFHEAL_STATE:-/var/run/valkey-selfheal.state}"
JOURNAL="${SELFHEAL_JOURNAL:-/var/log/valkey-selfheal.jsonl}"
JOURNAL_MAX_BYTES="${SELFHEAL_JOURNAL_MAX_BYTES:-1048576}"
JOURNAL_FIELD_MAX_CHARS="${SELFHEAL_JOURNAL_FIELD_MAX_CHARS:-2048}"
CRASH_RESTART_DELTA="${CRASH_RESTART_DELTA:-3}"      # 평가 간격 내 재시작 N회 이상 → crash loop
PING_FAIL_THRESHOLD="${PING_FAIL_THRESHOLD:-3}"      # ping 연속 실패 N회 → 장애
SETTLE_SEC="${RESTART_SETTLE_SEC:-5}"               # 각 복구 후 회복 확인 대기
RECOVERY_COOLDOWN_SEC="${RECOVERY_COOLDOWN_SEC:-60}"
SCRIPT_REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
STATE_LIB="${SCRIPT_REPO_DIR}/scripts/ops/lib/valkey-selfheal-state.sh"
JOURNAL_LIB="${SCRIPT_REPO_DIR}/scripts/ops/lib/valkey-selfheal-journal.sh"
REPO_DIR="${REPO_DIR:-${SCRIPT_REPO_DIR}}"
COMPOSE_FILE="${COMPOSE_FILE:-${REPO_DIR}/deploy/compose/docker-compose.prod.yml}"
COMPOSE_ENV_FILE="${COMPOSE_ENV_FILE:-/etc/stack-secrets/hololive-bot/compose.env}"
COMPOSE_RUNNER="${REPO_DIR}/scripts/deploy/compose.sh"
PING_SOCKET="${PING_SOCKET:-/var/run/valkey/valkey-cache.sock}"
# post-check: 복구 후 살아있어야 할 핵심 컨테이너
POSTCHECK_CONTAINERS="${POSTCHECK_CONTAINERS:-valkey-cache holo-postgres hololive-api hololive-alarm-worker}"
NOW="${SELFHEAL_NOW:-$(date +%s)}"
MODE="${1:---dry-run}"

# 1차 복구 순서.
RECOVERY_TIERS=(restart recreate)

case "${MODE}" in --dry-run|--apply) ;; *) echo "Usage: $0 [--dry-run|--apply]" >&2; exit 2 ;; esac

for numeric_setting in CRASH_RESTART_DELTA PING_FAIL_THRESHOLD SETTLE_SEC RECOVERY_COOLDOWN_SEC NOW JOURNAL_MAX_BYTES JOURNAL_FIELD_MAX_CHARS; do
  numeric_value="${!numeric_setting}"
  if [[ ! "${numeric_value}" =~ ^[0-9]+$ ]]; then
    echo "${numeric_setting} must be a non-negative integer" >&2
    exit 2
  fi
  if [ "${#numeric_value}" -gt 18 ]; then
    echo "${numeric_setting} exceeds the supported integer range" >&2
    exit 2
  fi
done
if [ "${PING_FAIL_THRESHOLD}" -eq 0 ] || [ "${CRASH_RESTART_DELTA}" -eq 0 ] ||
   [ "${JOURNAL_MAX_BYTES}" -eq 0 ] || [ "${JOURNAL_FIELD_MAX_CHARS}" -eq 0 ]; then
  echo "thresholds and journal bounds must be positive" >&2
  exit 2
fi
if [ "${JOURNAL_MAX_BYTES}" -lt 32768 ] || [ "${JOURNAL_FIELD_MAX_CHARS}" -gt 2048 ]; then
  echo "SELFHEAL_JOURNAL_MAX_BYTES must be at least 32768 and SELFHEAL_JOURNAL_FIELD_MAX_CHARS at most 2048" >&2
  exit 2
fi
for text_setting in VALKEY_CONTAINER STATE_FILE JOURNAL REPO_DIR COMPOSE_FILE COMPOSE_ENV_FILE PING_SOCKET POSTCHECK_CONTAINERS; do
  text_value="${!text_setting}"
  if [[ "${text_value}" =~ [[:cntrl:]] ]]; then
    echo "${text_setting} must not contain control characters" >&2
    exit 2
  fi
done
if [[ ! "${VALKEY_CONTAINER}" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]]; then
  echo "VALKEY_CONTAINER must be one Docker container name" >&2
  exit 2
fi
read -r -a validated_postcheck_containers <<<"${POSTCHECK_CONTAINERS}"
for postcheck_container in "${validated_postcheck_containers[@]}"; do
  if [[ ! "${postcheck_container}" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]]; then
    echo "POSTCHECK_CONTAINERS must contain only Docker container names" >&2
    exit 2
  fi
done

# shellcheck source=scripts/ops/lib/valkey-selfheal-journal.sh
. "${JOURNAL_LIB}"

input_validation_failed() {
  local name="$1" reason="$2" path="${3:-}" expected="${4:-}"
  journal "input_validation_failed" "$(printf '{"input":"%s","reason":"%s","path":%s,"expected":%s}' "${name}" "${reason}" "$(jstr "${path}")" "$(jstr "${expected}")")"
}

path_under_prefix() {
  local path="$1" prefix="$2"
  [ "${path}" = "${prefix}" ] || [[ "${path}" == "${prefix}/"* ]]
}

reject_if_insecure_metadata() {
  local name="$1" path="$2" owner mode_hex

  owner="$(stat -c '%u' -- "${path}" 2>/dev/null)" || { input_validation_failed "${name}" "stat_failed" "${path}" ""; return 1; }
  if [ "${owner}" != "0" ] && [ "${owner}" != "$(id -u)" ]; then
    input_validation_failed "${name}" "invalid_owner" "${path}" "root_or_current_user"
    return 1
  fi

  mode_hex="$(stat -c '%f' -- "${path}" 2>/dev/null)" || { input_validation_failed "${name}" "stat_failed" "${path}" ""; return 1; }
  if (( (0x${mode_hex} & 0x0012) != 0 )); then
    input_validation_failed "${name}" "group_or_world_writable" "${path}" "not_group_or_world_writable"
    return 1
  fi
}

validate_repo_dir() {
  local repo_real

  repo_real="$(realpath -e -- "${REPO_DIR}" 2>/dev/null)" || { input_validation_failed "REPO_DIR" "not_canonical" "${REPO_DIR}" "${SCRIPT_REPO_DIR}"; return 1; }
  if [ "${REPO_DIR}" != "${repo_real}" ]; then
    input_validation_failed "REPO_DIR" "not_canonical" "${REPO_DIR}" "${repo_real}"
    return 1
  fi
  if [ ! -d "${repo_real}" ]; then
    input_validation_failed "REPO_DIR" "not_directory" "${REPO_DIR}" "${SCRIPT_REPO_DIR}"
    return 1
  fi
  if [ "${repo_real}" != "${SCRIPT_REPO_DIR}" ]; then
    input_validation_failed "REPO_DIR" "outside_expected_prefix" "${REPO_DIR}" "${SCRIPT_REPO_DIR}"
    return 1
  fi
  reject_if_insecure_metadata "REPO_DIR" "${repo_real}" || return 1
  REPO_DIR="${repo_real}"
}

validate_compose_env_file() {
  local env_real prefix prefix_real allowed=0
  local prefixes=("${REPO_DIR}" "/etc/stack-secrets/hololive-bot")

  env_real="$(realpath -e -- "${COMPOSE_ENV_FILE}" 2>/dev/null)" || { input_validation_failed "COMPOSE_ENV_FILE" "not_canonical" "${COMPOSE_ENV_FILE}" "${REPO_DIR},/etc/stack-secrets/hololive-bot"; return 1; }
  if [ "${COMPOSE_ENV_FILE}" != "${env_real}" ]; then
    input_validation_failed "COMPOSE_ENV_FILE" "not_canonical" "${COMPOSE_ENV_FILE}" "${env_real}"
    return 1
  fi
  if [ ! -f "${env_real}" ]; then
    input_validation_failed "COMPOSE_ENV_FILE" "not_file" "${COMPOSE_ENV_FILE}" "${REPO_DIR},/etc/stack-secrets/hololive-bot"
    return 1
  fi

  for prefix in "${prefixes[@]}"; do
    prefix_real="$(realpath -m -- "${prefix}" 2>/dev/null)" || continue
    if path_under_prefix "${env_real}" "${prefix_real}"; then
      allowed=1
      break
    fi
  done
  if [ "${allowed}" -ne 1 ]; then
    input_validation_failed "COMPOSE_ENV_FILE" "outside_expected_prefix" "${COMPOSE_ENV_FILE}" "${REPO_DIR},/etc/stack-secrets/hololive-bot"
    return 1
  fi

  reject_if_insecure_metadata "COMPOSE_ENV_FILE" "${env_real}" || return 1
  COMPOSE_ENV_FILE="${env_real}"
}

validate_recovery_inputs() {
  validate_repo_dir || return 1
  validate_compose_env_file || return 1
}

recovery_argv_json() {
  local tier="$1"
  case "${tier}" in
    restart) json_array docker restart "${VALKEY_CONTAINER}" ;;
    recreate) json_array env "COMPOSE_ENV_FILE=${COMPOSE_ENV_FILE}" "${COMPOSE_RUNNER}" -f "${COMPOSE_FILE}" up -d --force-recreate --no-deps "${VALKEY_CONTAINER}" ;;
    *) json_array ;;
  esac
}

recovery_cmd() {
  local tier="$1"
  case "${tier}" in
    restart) printf 'docker restart %s' "${VALKEY_CONTAINER}" ;;
    recreate) printf 'COMPOSE_ENV_FILE=%s %s -f %s up -d --force-recreate --no-deps %s' "${COMPOSE_ENV_FILE}" "${COMPOSE_RUNNER}" "${COMPOSE_FILE}" "${VALKEY_CONTAINER}" ;;
    *) printf '' ;;
  esac
}

recovery_failed_detail() {
  local tier="$1" extra="$2" argv_json cmd
  argv_json="$(recovery_argv_json "${tier}")"
  cmd="$(recovery_cmd "${tier}")"
  printf '{"tier":"%s",%s,"cmd":%s,"argv":%s}' "${tier}" "${extra}" "$(jstr "${cmd}")" "${argv_json}"
}

RECOVERY_ACTION_GUARD_FAILED=0
run_recovery_action() {
  local tier="$1" action_status=0
  if ! state_lock_token_owned; then
    RECOVERY_ACTION_GUARD_FAILED=1
    state_transaction_failed action_lock_precheck
    return 125
  fi
  case "${tier}" in
    restart) docker restart "${VALKEY_CONTAINER}" || action_status="$?" ;;
    recreate) COMPOSE_ENV_FILE="${COMPOSE_ENV_FILE}" "${COMPOSE_RUNNER}" -f "${COMPOSE_FILE}" up -d --force-recreate --no-deps "${VALKEY_CONTAINER}" || action_status="$?" ;;
    *) return 2 ;;
  esac
  if ! state_lock_token_owned; then
    RECOVERY_ACTION_GUARD_FAILED=1
    state_transaction_failed action_lock_postcheck
    return 125
  fi
  return "${action_status}"
}

# shellcheck source=scripts/ops/lib/valkey-selfheal-state.sh
. "${STATE_LIB}"

restart_count() {
  local count
  count="$(docker inspect -f '{{.RestartCount}}' "${VALKEY_CONTAINER}" 2>/dev/null)" || { printf '%s\n' -1; return; }
  if [[ "${count}" =~ ^[0-9]+$ ]] && [ "${#count}" -le 18 ]; then printf '%s\n' "${count}"; else printf '%s\n' -1; fi
}
dry_run_probe_ok() {
  local state
  state="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "${VALKEY_CONTAINER}" 2>/dev/null || true)"
  case "${state}" in
    healthy|running) return 0 ;;
    unhealthy|starting|exited|dead|paused|restarting|created|removing) return 1 ;;
  esac
  docker ps --filter "name=^${VALKEY_CONTAINER}$" --filter status=running -q 2>/dev/null | grep -q .
}
ping_ok() {
  local container_ping_command captured payload pipeline_status
  if [ "${MODE}" != "--apply" ]; then dry_run_probe_ok; return; fi
  container_ping_command='REDISCLI_AUTH="$CACHE_PASSWORD" exec valkey-cli -s "$1" ping'
  captured="$(
    env -u CACHE_PASSWORD -u REDISCLI_AUTH \
      docker exec "${VALKEY_CONTAINER}" sh -c "${container_ping_command}" \
        valkey-selfheal "${PING_SOCKET}" 2>/dev/null | head -c 17
    pipeline_status="${PIPESTATUS[0]}:${PIPESTATUS[1]}"
    printf '\036%s' "${pipeline_status}"
  )"
  pipeline_status="${captured##*$'\036'}"
  payload="${captured:0:${#captured}-${#pipeline_status}-1}"
  [ "${pipeline_status}" = 0:0 ] || return 1
  case "${payload}" in
    PONG|$'PONG\n'|$'PONG\r\n') return 0 ;;
    *) return 1 ;;
  esac
}

# 복구 실행 + 회복 확인. 성공(ping PONG) 시 0.
try_recover() {
  local name="$1" argv_json cmd action_status=0
  argv_json="$(recovery_argv_json "${name}")"
  cmd="$(recovery_cmd "${name}")"
  if [ "${MODE}" != "--apply" ]; then journal "recover_skipped_dry_run" "$(printf '{"tier":"%s","cmd":%s,"argv":%s}' "${name}" "$(jstr "${cmd}")" "${argv_json}")"; return 1; fi
  journal "recover_exec" "$(printf '{"tier":"%s","cmd":%s,"argv":%s}' "${name}" "$(jstr "${cmd}")" "${argv_json}")" || return 1
  RECOVERY_ACTION_GUARD_FAILED=0
  run_recovery_action "${name}" >/dev/null 2>&1 || action_status="$?"
  [ "${RECOVERY_ACTION_GUARD_FAILED}" -eq 0 ] || return 2
  sleep "${SETTLE_SEC}" 2>/dev/null || true
  if ping_ok; then journal "recover_ok" "$(printf '{"tier":"%s"}' "${name}")"; return 0; fi
  journal "recover_no_recovery" "$(printf '{"tier":"%s","action_status":%s}' "${name}" "${action_status}")"; return 1
}

post_check() {
  local vk="down" adj="ok" c
  local postcheck_containers=()
  local IFS=' '
  read -r -a postcheck_containers <<<"${POSTCHECK_CONTAINERS}"
  ping_ok && vk="up"
  for c in "${postcheck_containers[@]}"; do
    [ -n "${c}" ] || continue
    docker ps --filter "name=^${c}$" --filter status=running -q 2>/dev/null | grep -q . \
      || { adj="degraded"; journal "post_check_core_missing" "$(printf '{"container":"%s"}' "${c}")"; }
  done
  journal "post_check" "$(printf '{"valkey":"%s","adjacent":"%s"}' "${vk}" "${adj}")"
}

# --- 평가 ---
if [ "${MODE}" = "--apply" ]; then
  if [ -L "${STATE_FILE}.lock" ] || { [ -e "${STATE_FILE}.lock" ] && [ ! -f "${STATE_FILE}.lock" ]; }; then
    journal "state_lock_failed" "$(printf '{"path":%s}' "$(jstr "${STATE_FILE}.lock")")"
    exit 1
  fi
  if ! { exec {STATE_LOCK_FD}>>"${STATE_FILE}.lock"; } 2>/dev/null; then
    journal "state_lock_failed" "$(printf '{"path":%s}' "$(jstr "${STATE_FILE}.lock")")"
    exit 1
  fi
  if ! flock -n "${STATE_LOCK_FD}"; then
    journal "state_lock_busy"
    exit 0
  fi
  if ! state_lock_fd_valid; then
    journal "state_lock_failed" "$(printf '{"path":%s}' "$(jstr "${STATE_FILE}.lock")")"
    exit 1
  fi
fi

load_state
if state_lock_token_mismatch; then
  state_transaction_failed lock_token_mismatch
  exit 1
fi
RC_NOW=$(restart_count); [ "${RC_NOW}" -lt 0 ] && RC_NOW=${RC_PREV}
RESTART_DELTA=$(( RC_NOW - RC_PREV )); [ "${RESTART_DELTA}" -lt 0 ] && RESTART_DELTA=0
if ping_ok; then
  if [ "${STATE_VALID}" -ne 1 ]; then
    journal "state_invalid_healthy_reset"
  fi
  reset_healthy_state || exit 1
  if healthy_state_changed; then journal "healthy_reset"; else journal_observe "healthy"; fi
  exit 0
fi
if [ "${STATE_VALID}" -ne 1 ]; then
  journal "state_invalid" "$(printf '{"terminal":"manual_intervention_required"}')"
  exit 1
fi
if [ "${PING_FAIL}" -ge 9223372036854775807 ]; then
  journal "state_invalid" "$(printf '{"terminal":"counter_range_exhausted"}')"
  exit 1
fi
PING_FAIL_NOW=$(( PING_FAIL + 1 ))

CRASH_LOOP=0
[ "${RESTART_DELTA}" -ge "${CRASH_RESTART_DELTA}" ] && CRASH_LOOP=1
[ "${PING_FAIL_NOW}" -ge "${PING_FAIL_THRESHOLD}" ] && CRASH_LOOP=1
journal "evaluate" "$(printf '{"rc_now":%s,"rc_prev":%s,"restart_delta":%s,"ping_fail":%s,"crash_loop":%s}' "${RC_NOW}" "${RC_PREV}" "${RESTART_DELTA}" "${PING_FAIL_NOW}" "${CRASH_LOOP}")" || exit 1
RC_PREV="${RC_NOW}"
PING_FAIL="${PING_FAIL_NOW}"
if [ "${CRASH_LOOP}" -eq 0 ]; then persist_state || exit 1; exit 0; fi

if [ "${RECOVERY_STATUS}" = "manual_intervention_required" ]; then
  journal "manual_intervention_required" "$(printf '{"epoch":%s,"mutations":%s}' "${RECOVERY_EPOCH}" "${RECOVERY_MUTATIONS}")"
  exit 1
fi

if [ "${RECOVERY_EPOCH}" -eq 0 ]; then
  RECOVERY_EPOCH="${NOW}"
  [ "${RECOVERY_EPOCH}" -gt 0 ] || RECOVERY_EPOCH=1
  RECOVERY_MUTATIONS=0
  COOLDOWN_UNTIL=0
fi

if [ "${NOW}" -lt "${COOLDOWN_UNTIL}" ]; then
  journal "recovery_cooldown" "$(printf '{"epoch":%s,"mutations":%s,"until":%s}' "${RECOVERY_EPOCH}" "${RECOVERY_MUTATIONS}" "${COOLDOWN_UNTIL}")"
  exit 1
fi

if [ "${MODE}" != "--apply" ]; then
  next_tier="${RECOVERY_TIERS[${RECOVERY_MUTATIONS}]:-manual_intervention_required}"
  journal "recover_skipped_dry_run" "$(printf '{"tier":"%s","epoch":%s,"mutations":%s}' "${next_tier}" "${RECOVERY_EPOCH}" "${RECOVERY_MUTATIONS}")"
  exit 1
fi

# --- 1차: 고정 dispatch 에스컬레이션 ---
if [ "${MODE}" = "--apply" ] && ! validate_recovery_inputs; then
  journal "recover_failed" "$(recovery_failed_detail recreate '"reason":"input_validation_failed"')"
  post_check
  exit 1
fi

if [ "${RECOVERY_MUTATIONS}" -ge 2 ]; then
  journal "manual_intervention_required" "$(printf '{"epoch":%s,"mutations":%s}' "${RECOVERY_EPOCH}" "${RECOVERY_MUTATIONS}")"
  exit 1
fi

name="${RECOVERY_TIERS[${RECOVERY_MUTATIONS}]}"
reserve_mutation || exit 1
if try_recover "${name}"; then
  post_check
  reset_healthy_state || exit 1
  exit 0
fi
[ "${RECOVERY_ACTION_GUARD_FAILED}" -eq 0 ] || exit 1

if [ "${RECOVERY_MUTATIONS}" -lt 2 ]; then
  journal "recovery_cooldown_started" "$(printf '{"epoch":%s,"mutations":%s,"until":%s}' "${RECOVERY_EPOCH}" "${RECOVERY_MUTATIONS}" "${COOLDOWN_UNTIL}")"
  post_check
  exit 1
fi

journal "recover_failed" "$(recovery_failed_detail "${name}" '"terminal":"manual_intervention_required"')"
journal "manual_intervention_required" "$(printf '{"epoch":%s,"mutations":%s}' "${RECOVERY_EPOCH}" "${RECOVERY_MUTATIONS}")"
post_check
exit 1
