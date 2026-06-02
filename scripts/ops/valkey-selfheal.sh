#!/usr/bin/env bash
# valkey-selfheal.sh — valkey-cache crash loop 능동 자가복구 watchdog (1회 평가)
#
# 호출 전제: systemd timer 등으로 주기 실행(예: 30s). 상태는 STATE_FILE 에 persist.
# 기본 --dry-run(감지/저널만, 복구 미실행). 실제 복구는 --apply.
#
# 복구 단계(능동):
#   1차 결정론(빠름·로컬, 외부의존 0): docker restart → compose force-recreate. 대부분 흡수.
#   2차 codex 자율복구: 1차로도 회복 못 하면 codex 가 직접 진단+복구한다
#        (--sandbox danger-full-access). 화이트리스트 없음 — 복구 판단/실행을 codex 에 위임.
#
# 주의: 2차는 codex 가 production 호스트(docker 그룹)에서 자율로 명령을 실행한다.
#       사용자 승인된 능동복구 모드다. 능동성과 충돌하지 않는 최소 안전망만 강제한다:
#   - debounce / timeout / per-pass 1회 : codex 호출 폭주·무한 점유 차단(무한 재기동에 물림 방지)
#   - journal(JSONL)                    : 프롬프트·codex 전체 출력·모든 tier 동작 보존(사후추적)
#   - post-check                        : 복구 후 valkey + 인접 서비스 health 재확인(codex 부작용 감지)
#   - 프롬프트 범위한정                 : valkey-cache 복구로 한정, PG-first 라 데이터 파괴 불필요 명시

set -uo pipefail

VALKEY_CONTAINER="${VALKEY_CONTAINER:-valkey-cache}"
STATE_FILE="${SELFHEAL_STATE:-/var/run/valkey-selfheal.state}"
JOURNAL="${SELFHEAL_JOURNAL:-/var/log/valkey-selfheal.jsonl}"
CRASH_RESTART_DELTA="${CRASH_RESTART_DELTA:-3}"      # 평가 간격 내 재시작 N회 이상 → crash loop
PING_FAIL_THRESHOLD="${PING_FAIL_THRESHOLD:-3}"      # ping 연속 실패 N회 → 장애
CODEX_DEBOUNCE_SEC="${CODEX_DEBOUNCE_SEC:-900}"      # codex 재호출 최소 간격
CODEX_TIMEOUT_SEC="${CODEX_TIMEOUT_SEC:-300}"
CODEX_MODEL="${SELFHEAL_CODEX_MODEL:-}"              # 비우면 codex 기본 모델
SETTLE_SEC="${RESTART_SETTLE_SEC:-5}"               # 각 복구 후 회복 확인 대기
REPO_DIR="${REPO_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
COMPOSE_FILE="${COMPOSE_FILE:-${REPO_DIR}/docker-compose.prod.yml}"
COMPOSE_ENV_FILE="${COMPOSE_ENV_FILE:-/run/hololive-bot/env}"
PING_SOCKET="${PING_SOCKET:-/var/run/valkey/valkey-cache.sock}"
# post-check: codex 복구 후 살아있어야 할 핵심 컨테이너(codex 부작용으로 사라졌는지 감지)
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
  local detail="${2:-{}}"
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

# --- 2차: codex 자율복구 (debounce) ---
SINCE_CODEX=$(( NOW - LAST_CODEX ))
if [ "${SINCE_CODEX}" -lt "${CODEX_DEBOUNCE_SEC}" ]; then
  journal "codex_debounced" "$(printf '{"since_sec":%s,"debounce_sec":%s}' "${SINCE_CODEX}" "${CODEX_DEBOUNCE_SEC}")"
  write_state "${RC_NOW}" "${PING_FAIL_NOW}" "${LAST_CODEX}"; exit 0
fi

RECENT_LOG=$(docker logs --tail 60 "${VALKEY_CONTAINER}" 2>&1 | tr '\n' ' ' | cut -c1-2000)
PROMPT="너는 production 호스트(docker 그룹 권한)에서 직접 명령을 실행해 '${VALKEY_CONTAINER}' 컨테이너의 crash loop 를 복구하는 SRE 다.
1차 자동복구(docker restart, compose force-recreate)로도 회복하지 못했다. 직접 진단하고 필요한 복구를 능동적으로 수행하라.
제약:
 1) 복구 대상은 '${VALKEY_CONTAINER}' 컨테이너로만 한정한다. 다른 컨테이너/서비스/호스트 설정을 절대 건드리지 마라.
 2) 이 시스템은 PG-first 다 — valkey 데이터는 PostgreSQL 이 source 이고 휘발 캐시는 앱이 PG 에서 다시 warm 한다. 따라서 데이터 보존을 위한 파괴적 조작은 불필요하다.
 3) 복구를 마치면 해당 컨테이너에서 valkey-cli ping 으로 PONG 을 확인하고, 무엇을 왜 했는지 마지막에 'SUMMARY: ...' 한 줄로 요약하라.
근거: docker inspect/logs 로 ${VALKEY_CONTAINER} 의 실행 command 와 종료 원인을 확인하라.
최근 로그: ${RECENT_LOG}"

journal "codex_invoke" "$(printf '{"timeout_sec":%s,"sandbox":"danger-full-access"}' "${CODEX_TIMEOUT_SEC}")"
if [ "${MODE}" = "--apply" ]; then
  CODEX_ARGS=(exec --sandbox danger-full-access --skip-git-repo-check --cd "${REPO_DIR}")
  [ -n "${CODEX_MODEL}" ] && CODEX_ARGS+=(--model "${CODEX_MODEL}")
  CODEX_OUT=$(timeout "${CODEX_TIMEOUT_SEC}" codex "${CODEX_ARGS[@]}" "${PROMPT}" 2>&1); CODEX_RC=$?
  journal "codex_result" "$(printf '{"exit":%s,"output":%s}' "${CODEX_RC}" "$(jstr "${CODEX_OUT}")")"
  post_check
else
  journal "codex_skipped_dry_run" "$(printf '{"prompt_preview":%s}' "$(jstr "$(printf '%s' "${PROMPT}" | cut -c1-200)")")"
fi
write_state "$(restart_count)" "${PING_FAIL_NOW}" "${NOW}"
exit 0
