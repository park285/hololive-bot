#!/usr/bin/env bash
# valkey-selfheal.sh — valkey-cache crash loop 능동 자가복구 watchdog (1회 평가)
#
# 호출 전제: systemd timer 등으로 주기 실행(예: 30s). 상태는 STATE_FILE 에 persist.
# 기본 --dry-run(감지/저널만, 복구 미실행). 실제 복구는 --apply.
#
# 복구 단계(능동):
#   고정 dispatch(빠름·로컬, 외부의존 0): docker restart → compose force-recreate.

set -uo pipefail

VALKEY_CONTAINER="${VALKEY_CONTAINER:-valkey-cache}"
STATE_FILE="${SELFHEAL_STATE:-/var/run/valkey-selfheal.state}"
JOURNAL="${SELFHEAL_JOURNAL:-/var/log/valkey-selfheal.jsonl}"
CRASH_RESTART_DELTA="${CRASH_RESTART_DELTA:-3}"      # 평가 간격 내 재시작 N회 이상 → crash loop
PING_FAIL_THRESHOLD="${PING_FAIL_THRESHOLD:-3}"      # ping 연속 실패 N회 → 장애
SETTLE_SEC="${RESTART_SETTLE_SEC:-5}"               # 각 복구 후 회복 확인 대기
SCRIPT_REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
REPO_DIR="${REPO_DIR:-${SCRIPT_REPO_DIR}}"
COMPOSE_FILE="${COMPOSE_FILE:-${REPO_DIR}/docker-compose.prod.yml}"
COMPOSE_ENV_FILE="${COMPOSE_ENV_FILE:-/run/hololive-bot/env}"
PING_SOCKET="${PING_SOCKET:-/var/run/valkey/valkey-cache.sock}"
# post-check: 복구 후 살아있어야 할 핵심 컨테이너
POSTCHECK_CONTAINERS="${POSTCHECK_CONTAINERS:-valkey-cache holo-postgres hololive-kakao-bot-go hololive-alarm-worker}"
NOW="${SELFHEAL_NOW:-$(date +%s)}"
MODE="${1:---dry-run}"

# 1차 복구 순서.
RECOVERY_TIERS=(restart recreate)

case "${MODE}" in --dry-run|--apply) ;; *) echo "Usage: $0 [--dry-run|--apply]" >&2; exit 2 ;; esac

journal() {
  local detail="${2:-}"; [ -n "${detail}" ] || detail='{}'   # ${2:-{}} 는 bash 가 trailing '}' 로 깨므로 분리
  local line; line=$(printf '{"ts":%s,"mode":"%s","event":"%s","detail":%s}' "${NOW}" "${MODE}" "$1" "${detail}")
  [ "${MODE}" = "--apply" ] && { printf '%s\n' "${line}" >>"${JOURNAL}" 2>/dev/null || true; }
  printf '[selfheal] %s\n' "${line}" >&2
}
jstr() { printf '%s' "$1" | python3 -c 'import json,sys;sys.stdout.write(json.dumps(sys.stdin.read()))' 2>/dev/null || printf '"%s"' "$(printf '%s' "$1" | tr -d '"\n')"; }
json_array() {
  local first=1 item
  printf '['
  for item in "$@"; do
    if [ "${first}" -eq 1 ]; then first=0; else printf ','; fi
    jstr "${item}"
  done
  printf ']'
}

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
  local prefixes=("${REPO_DIR}" "/run/hololive-bot")

  env_real="$(realpath -e -- "${COMPOSE_ENV_FILE}" 2>/dev/null)" || { input_validation_failed "COMPOSE_ENV_FILE" "not_canonical" "${COMPOSE_ENV_FILE}" "${REPO_DIR},/run/hololive-bot"; return 1; }
  if [ "${COMPOSE_ENV_FILE}" != "${env_real}" ]; then
    input_validation_failed "COMPOSE_ENV_FILE" "not_canonical" "${COMPOSE_ENV_FILE}" "${env_real}"
    return 1
  fi
  if [ ! -f "${env_real}" ]; then
    input_validation_failed "COMPOSE_ENV_FILE" "not_file" "${COMPOSE_ENV_FILE}" "${REPO_DIR},/run/hololive-bot"
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
    input_validation_failed "COMPOSE_ENV_FILE" "outside_expected_prefix" "${COMPOSE_ENV_FILE}" "${REPO_DIR},/run/hololive-bot"
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
    recreate) json_array docker compose --env-file "${COMPOSE_ENV_FILE}" -f "${COMPOSE_FILE}" up -d --force-recreate --no-deps "${VALKEY_CONTAINER}" ;;
    *) json_array ;;
  esac
}

recovery_cmd() {
  local tier="$1"
  case "${tier}" in
    restart) printf 'docker restart %s' "${VALKEY_CONTAINER}" ;;
    recreate) printf 'docker compose --env-file %s -f %s up -d --force-recreate --no-deps %s' "${COMPOSE_ENV_FILE}" "${COMPOSE_FILE}" "${VALKEY_CONTAINER}" ;;
    *) printf '' ;;
  esac
}

recovery_failed_detail() {
  local tier="$1" extra="$2" argv_json cmd
  argv_json="$(recovery_argv_json "${tier}")"
  cmd="$(recovery_cmd "${tier}")"
  printf '{"tier":"%s",%s,"cmd":%s,"argv":%s}' "${tier}" "${extra}" "$(jstr "${cmd}")" "${argv_json}"
}

run_recovery_action() {
  local tier="$1"
  case "${tier}" in
    restart) docker restart "${VALKEY_CONTAINER}" ;;
    recreate) docker compose --env-file "${COMPOSE_ENV_FILE}" -f "${COMPOSE_FILE}" up -d --force-recreate --no-deps "${VALKEY_CONTAINER}" ;;
    *) return 2 ;;
  esac
}

read_state() { [ -r "${STATE_FILE}" ] && cat "${STATE_FILE}" || echo "0 0 0"; }
write_state() { [ "${MODE}" = "--apply" ] && { printf '%s %s %s\n' "$1" "$2" "$3" >"${STATE_FILE}" 2>/dev/null || true; }; return 0; }
restart_count() { docker inspect -f '{{.RestartCount}}' "${VALKEY_CONTAINER}" 2>/dev/null || echo -1; }
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
  if [ "${MODE}" != "--apply" ]; then dry_run_probe_ok; return; fi
  local pw; pw=$(docker exec "${VALKEY_CONTAINER}" printenv CACHE_PASSWORD 2>/dev/null || echo "")
  docker exec -e "REDISCLI_AUTH=${pw}" "${VALKEY_CONTAINER}" valkey-cli -s "${PING_SOCKET}" ping 2>/dev/null | grep -q PONG
}

# 복구 실행 + 회복 확인. 성공(ping PONG) 시 0.
try_recover() {
  local name="$1" argv_json cmd
  argv_json="$(recovery_argv_json "${name}")"
  cmd="$(recovery_cmd "${name}")"
  if [ "${MODE}" != "--apply" ]; then journal "recover_skipped_dry_run" "$(printf '{"tier":"%s","cmd":%s,"argv":%s}' "${name}" "$(jstr "${cmd}")" "${argv_json}")"; return 1; fi
  journal "recover_exec" "$(printf '{"tier":"%s","cmd":%s,"argv":%s}' "${name}" "$(jstr "${cmd}")" "${argv_json}")"
  run_recovery_action "${name}" >/dev/null 2>&1
  sleep "${SETTLE_SEC}" 2>/dev/null || true
  if ping_ok; then journal "recover_ok" "$(printf '{"tier":"%s"}' "${name}")"; return 0; fi
  journal "recover_no_recovery" "$(printf '{"tier":"%s"}' "${name}")"; return 1
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
read -r RC_PREV PING_FAIL LAST_CODEX <<<"$(read_state)"
RC_NOW=$(restart_count); [ "${RC_NOW}" -lt 0 ] && RC_NOW=${RC_PREV}
RESTART_DELTA=$(( RC_NOW - RC_PREV )); [ "${RESTART_DELTA}" -lt 0 ] && RESTART_DELTA=0
if ping_ok; then PING_FAIL_NOW=0; else PING_FAIL_NOW=$(( PING_FAIL + 1 )); fi

CRASH_LOOP=0
[ "${RESTART_DELTA}" -ge "${CRASH_RESTART_DELTA}" ] && CRASH_LOOP=1
[ "${PING_FAIL_NOW}" -ge "${PING_FAIL_THRESHOLD}" ] && CRASH_LOOP=1
journal "evaluate" "$(printf '{"rc_now":%s,"rc_prev":%s,"restart_delta":%s,"ping_fail":%s,"crash_loop":%s}' "${RC_NOW}" "${RC_PREV}" "${RESTART_DELTA}" "${PING_FAIL_NOW}" "${CRASH_LOOP}")"
if [ "${CRASH_LOOP}" -eq 0 ]; then write_state "${RC_NOW}" "${PING_FAIL_NOW}" "${LAST_CODEX}"; exit 0; fi

# --- 1차: 고정 dispatch 에스컬레이션 ---
if [ "${MODE}" = "--apply" ] && ! validate_recovery_inputs; then
  journal "recover_failed" "$(recovery_failed_detail recreate '"reason":"input_validation_failed"')"
  post_check
  write_state "$(restart_count)" "${PING_FAIL_NOW}" "${LAST_CODEX}"
  exit 1
fi

for name in "${RECOVERY_TIERS[@]}"; do
  if try_recover "${name}"; then post_check; write_state "$(restart_count)" 0 "${LAST_CODEX}"; exit 0; fi
done

journal "recover_failed" "$(recovery_failed_detail recreate '"tiers":"restart,recreate"')"
post_check
write_state "$(restart_count)" "${PING_FAIL_NOW}" "${LAST_CODEX}"
exit 1
