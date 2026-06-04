#!/usr/bin/env bash
# valkey-selfheal.sh — valkey-cache crash loop 능동 자가복구 watchdog (1회 평가)
#
# 호출 전제: systemd timer 등으로 주기 실행(예: 30s). 상태는 STATE_FILE 에 persist.
# 기본 --dry-run(감지/저널만, 복구 미실행). 실제 복구는 --apply.
#
# 복구 단계(능동):
#   결정론(빠름·로컬, 외부의존 0): docker restart → compose force-recreate.

set -uo pipefail

VALKEY_CONTAINER="${VALKEY_CONTAINER:-valkey-cache}"
STATE_FILE="${SELFHEAL_STATE:-/var/run/valkey-selfheal.state}"
JOURNAL="${SELFHEAL_JOURNAL:-/var/log/valkey-selfheal.jsonl}"
CRASH_RESTART_DELTA="${CRASH_RESTART_DELTA:-3}"      # 평가 간격 내 재시작 N회 이상 → crash loop
PING_FAIL_THRESHOLD="${PING_FAIL_THRESHOLD:-3}"      # ping 연속 실패 N회 → 장애
SETTLE_SEC="${RESTART_SETTLE_SEC:-5}"               # 각 복구 후 회복 확인 대기
REPO_DIR="${REPO_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
COMPOSE_FILE="${COMPOSE_FILE:-${REPO_DIR}/docker-compose.prod.yml}"
COMPOSE_ENV_FILE="${COMPOSE_ENV_FILE:-/run/hololive-bot/env}"
PING_SOCKET="${PING_SOCKET:-/var/run/valkey/valkey-cache.sock}"
# post-check: 복구 후 살아있어야 할 핵심 컨테이너
POSTCHECK_CONTAINERS="${POSTCHECK_CONTAINERS:-valkey-cache holo-postgres hololive-kakao-bot-go hololive-alarm-worker}"
NOW="${SELFHEAL_NOW:-$(date +%s)}"
MODE="${1:---dry-run}"

# 1차 결정론 복구 순서: name|command
DETERMINISTIC=(
  "restart|docker restart ${VALKEY_CONTAINER}"
  "recreate|docker compose --env-file ${COMPOSE_ENV_FILE} -f ${COMPOSE_FILE} up -d --force-recreate --no-deps ${VALKEY_CONTAINER}"
)

case "${MODE}" in --dry-run|--apply) ;; *) echo "Usage: $0 [--dry-run|--apply]" >&2; exit 2 ;; esac

journal() {
  local detail="${2:-}"; [ -n "${detail}" ] || detail='{}'   # ${2:-{}} 는 bash 가 trailing '}' 로 깨므로 분리
  local line; line=$(printf '{"ts":%s,"mode":"%s","event":"%s","detail":%s}' "${NOW}" "${MODE}" "$1" "${detail}")
  [ "${MODE}" = "--apply" ] && { printf '%s\n' "${line}" >>"${JOURNAL}" 2>/dev/null || true; }
  printf '[selfheal] %s\n' "${line}" >&2
}
jstr() { printf '%s' "$1" | python3 -c 'import json,sys;print(json.dumps(sys.stdin.read()))' 2>/dev/null || printf '"%s"' "$(printf '%s' "$1" | tr -d '"\n')"; }

read_state() { [ -r "${STATE_FILE}" ] && cat "${STATE_FILE}" || echo "0 0 0"; }
write_state() { [ "${MODE}" = "--apply" ] && { printf '%s %s %s\n' "$1" "$2" "$3" >"${STATE_FILE}" 2>/dev/null || true; }; return 0; }
restart_count() { docker inspect -f '{{.RestartCount}}' "${VALKEY_CONTAINER}" 2>/dev/null || echo -1; }
ping_ok() {
  local pw; pw=$(docker exec "${VALKEY_CONTAINER}" printenv CACHE_PASSWORD 2>/dev/null || echo "")
  docker exec "${VALKEY_CONTAINER}" sh -c "REDISCLI_AUTH='${pw}' valkey-cli -s ${PING_SOCKET} ping" 2>/dev/null | grep -q PONG
}

# 복구 실행 + 회복 확인. 성공(ping PONG) 시 0.
try_recover() {
  local name="$1" cmd="$2"
  if [ "${MODE}" != "--apply" ]; then journal "recover_skipped_dry_run" "$(printf '{"tier":"%s","cmd":%s}' "${name}" "$(jstr "${cmd}")")"; return 1; fi
  journal "recover_exec" "$(printf '{"tier":"%s","cmd":%s}' "${name}" "$(jstr "${cmd}")")"
  eval "${cmd}" >/dev/null 2>&1
  sleep "${SETTLE_SEC}" 2>/dev/null || true
  if ping_ok; then journal "recover_ok" "$(printf '{"tier":"%s"}' "${name}")"; return 0; fi
  journal "recover_no_recovery" "$(printf '{"tier":"%s"}' "${name}")"; return 1
}

post_check() {
  local vk="down" adj="ok" c
  ping_ok && vk="up"
  for c in ${POSTCHECK_CONTAINERS}; do
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

# --- 1차: 결정론 에스컬레이션 ---
for entry in "${DETERMINISTIC[@]}"; do
  name="${entry%%|*}"; cmd="${entry#*|}"
  if try_recover "${name}" "${cmd}"; then post_check; write_state "$(restart_count)" 0 "${LAST_CODEX}"; exit 0; fi
done

journal "recover_failed" "$(printf '{"tiers":"restart,recreate"}')"
post_check
write_state "$(restart_count)" "${PING_FAIL_NOW}" "${LAST_CODEX}"
exit 1
