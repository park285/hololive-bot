#!/usr/bin/env bash
set -euo pipefail
if [[ $# -ne 3 || ! "$1" =~ ^[0-9a-f]{64}$ || ! "$2" =~ ^[0-9a-f]{64}$ || ! "$3" =~ ^[0-9a-f]{64}$ ]]; then
  echo 'usage: purge-admin-dashboard-sessions.sh <valkey-id> <stopped-bff-id> <fenced-ingress-id>' >&2
  exit 2
fi
ADMIN_PURGE_CACHE="$1"
ADMIN_PURGE_BFF="$2"
ADMIN_PURGE_INGRESS="$3"
ADMIN_PURGE_OWNER="$(docker inspect --format '{{index .Config.Labels "io.hololive.admin-fixture"}}' "${ADMIN_PURGE_CACHE}")"
if [[ "${ADMIN_PURGE_OWNER}" =~ ^admin-lab-[a-z0-9-]+$ ]]; then
  for id in "${ADMIN_PURGE_BFF}" "${ADMIN_PURGE_INGRESS}"; do
    [[ "$(docker inspect --format '{{index .Config.Labels "io.hololive.admin-fixture"}}' "${id}")" == "${ADMIN_PURGE_OWNER}" ]]
  done
else
  [[ "$(docker inspect --format '{{.Name}}' "${ADMIN_PURGE_CACHE}")" == /valkey-cache ]]
  [[ "$(docker inspect --format '{{.Name}}' "${ADMIN_PURGE_BFF}")" == /admin-dashboard ]]
  [[ "$(docker inspect --format '{{.Name}}' "${ADMIN_PURGE_INGRESS}")" == /admin-dashboard-ingress ]]
  ADMIN_PURGE_PROJECT="$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "${ADMIN_PURGE_CACHE}")"
  [[ -n "${ADMIN_PURGE_PROJECT}" && "${ADMIN_PURGE_PROJECT}" != '<no value>' ]]
  [[ "$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "${ADMIN_PURGE_BFF}")" == "${ADMIN_PURGE_PROJECT}" ]]
fi
[[ "$(docker inspect --format '{{.State.Status}}' "${ADMIN_PURGE_BFF}")" == exited ]]
ADMIN_PURGE_CONFIG="$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/etc/nginx/admin-dashboard-ingress.conf"}}{{.Source}}{{end}}{{end}}' "${ADMIN_PURGE_INGRESS}")"
[[ "${ADMIN_PURGE_CONFIG}" == /* && -f "${ADMIN_PURGE_CONFIG}" && ! -L "${ADMIN_PURGE_CONFIG}" ]]
ADMIN_PURGE_MARKER="${ADMIN_PURGE_CONFIG}.maintenance"
[[ -f "${ADMIN_PURGE_MARKER}" && ! -L "${ADMIN_PURGE_MARKER}" ]]
[[ "$(stat -c '%u:%a' "${ADMIN_PURGE_MARKER}")" == "$(id -u):600" && "$(cat "${ADMIN_PURGE_MARKER}")" == closed ]]
ADMIN_PURGE_BIND="$(sed -nE 's/^[[:space:]]*listen ([0-9.]+):30191;/\1/p' "${ADMIN_PURGE_CONFIG}")"
[[ "${ADMIN_PURGE_BIND}" =~ ^[0-9]+(\.[0-9]+){3}$ ]]
[[ "$(curl --noproxy '*' --max-time 2 -s -o /dev/null -w '%{http_code}' "http://${ADMIN_PURGE_BIND}:30191/health")" == 503 ]]

ADMIN_PURGE_CURSOR=0
ADMIN_PURGE_REMOVED=0
ADMIN_PURGE_PAGES=0
ADMIN_PURGE_DEADLINE=$((SECONDS + 30))
while :; do
  (( SECONDS < ADMIN_PURGE_DEADLINE && ADMIN_PURGE_PAGES < 10000 )) || { echo 'admin session purge limit exceeded; outcome_unknown' >&2; exit 1; }
  # 인증 값은 기존 Valkey container 안에서만 소비합니다. Lua는 고정 prefix 밖의 key를 받지 않습니다.
  ADMIN_PURGE_RESULT="$(timeout --kill-after=2s 5s docker exec -i "${ADMIN_PURGE_CACHE}" sh -c \
    'export REDISCLI_AUTH="${CACHE_PASSWORD:?configured cache password required}"; exec valkey-cli --raw EVAL "$(cat)" 0 "$1"' \
    admin-session-purge "${ADMIN_PURGE_CURSOR}" <<'LUA'
local page = redis.call('SCAN', ARGV[1], 'MATCH', 'session:admin:*', 'COUNT', 1000)
if #page[2] > 4096 then return redis.error_reply('admin purge batch exceeds bound') end
local deleted = 0
for _, key in ipairs(page[2]) do
  if string.sub(key, 1, 14) ~= 'session:admin:' then return redis.error_reply('admin prefix mismatch') end
  deleted = deleted + redis.call('UNLINK', key)
end
return {page[1], deleted}
LUA
  )" || { echo 'admin session purge command failed; outcome_unknown; do not replay business actions' >&2; exit 1; }
  (( SECONDS < ADMIN_PURGE_DEADLINE )) || { echo 'admin session purge deadline exceeded; outcome_unknown' >&2; exit 1; }
  mapfile -t ADMIN_PURGE_LINES <<< "${ADMIN_PURGE_RESULT}"
  [[ "${#ADMIN_PURGE_LINES[@]}" == 2 && "${ADMIN_PURGE_LINES[0]}" =~ ^[0-9]+$ && "${ADMIN_PURGE_LINES[1]}" =~ ^[0-9]+$ ]] \
    || { echo 'admin session purge reply invalid; outcome_unknown' >&2; exit 1; }
  ADMIN_PURGE_CURSOR="${ADMIN_PURGE_LINES[0]}"
  ADMIN_PURGE_REMOVED=$((ADMIN_PURGE_REMOVED + ADMIN_PURGE_LINES[1]))
  ADMIN_PURGE_PAGES=$((ADMIN_PURGE_PAGES + 1))
  [[ "${ADMIN_PURGE_CURSOR}" != 0 ]] || break
done
printf 'admin_session_purge=completed prefix=session:admin: removed=%d scan_pages=%d\n' "${ADMIN_PURGE_REMOVED}" "${ADMIN_PURGE_PAGES}"
