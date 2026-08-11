#!/usr/bin/env bash

set -euo pipefail

PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH

CONFIG_FILE="${POSTGRES_FAILOVER_ROUTE_CONFIG_FILE:-/etc/hololive-postgres-failover/route.env}"
if [[ "${1:-}" == --config ]]; then
  [[ $# -ge 2 ]] || { printf '[postgres-route-tailscale] config path is required\n' >&2; exit 2; }
  CONFIG_FILE="$2"
  shift 2
fi

[[ $# -eq 5 || $# -eq 6 ]] || { printf '[postgres-route-tailscale] expected old host/port, new host/port, fence token, and optional Tailscale Service\n' >&2; exit 2; }
OLD_PRIMARY_HOST="$1"
OLD_PRIMARY_PORT="$2"
NEW_PRIMARY_HOST="$3"
NEW_PRIMARY_PORT="$4"
FENCE_TOKEN="$5"
TAILSCALE_SERVICE_ARGUMENT="${6:-}"

die() {
  printf '[postgres-route-tailscale] %s\n' "$1" >&2
  exit "${2:-1}"
}

is_host() {
  [[ "$1" =~ ^[A-Za-z0-9._:-]+$ ]]
}

is_port() {
  if [[ "$1" =~ ^[0-9]{1,5}$ ]]; then
    (( 10#$1 > 0 && 10#$1 <= 65535 ))
  else
    return 1
  fi
}

is_token() {
  [[ "$1" =~ ^[A-Za-z0-9._:-]{8,128}$ ]]
}

is_tailscale_service() {
  [[ "$1" =~ ^svc:[a-z0-9][a-z0-9-]{0,62}$ ]]
}

is_path() {
  [[ "$1" == /* && "$1" =~ ^/[A-Za-z0-9._/-]+$ && "$1" != *'/../'* && "$1" != *'/./'* && "$1" != *'//'* ]]
}

secure_path() {
  local label="$1" path="$2" private="$3" current owner mode_hex real
  is_path "${path}" || die "${label} path is invalid" 2
  real="$(/usr/bin/realpath -e -- "${path}")" || die "${label} is missing" 2
  [[ "${real}" == "${path}" && -f "${path}" && ! -L "${path}" ]] || die "${label} must be a canonical regular file" 2
  current="${path}"
  while :; do
    [[ ! -L "${current}" && -e "${current}" ]] || die "${label} path contains a symlink or missing component" 2
    owner="$(/usr/bin/stat -c '%u' -- "${current}")" || die "${label} ownership cannot be checked" 2
    [[ "${owner}" == 0 ]] || die "${label} path must be root-owned" 2
    mode_hex="$(/usr/bin/stat -c '%f' -- "${current}")" || die "${label} mode cannot be checked" 2
    if (( (0x${mode_hex} & 0x0012) != 0 )) && { [[ ! -d "${current}" || "${owner}" != 0 ]] || (( (0x${mode_hex} & 0x0200) == 0 )); }; then
      die "${label} path must not be group/world writable" 2
    fi
    [[ "${current}" == / ]] && break
    current="$(/usr/bin/dirname -- "${current}")"
  done
  if [[ "${private}" == 1 ]]; then
    mode_hex="$(/usr/bin/stat -c '%f' -- "${path}")" || die "${label} mode cannot be checked" 2
    (( (0x${mode_hex} & 0x003f) == 0 )) || die "${label} must not grant group/world permissions" 2
  fi
}

secure_optional_state_path() {
  local path="$1" current owner mode_hex
  is_path "${path}" || die "route state path is invalid" 2
  if [[ -e "${path}" || -L "${path}" ]]; then
    secure_path "route state" "${path}" 1
    return
  fi
  current="$(/usr/bin/dirname -- "${path}")"
  while :; do
    [[ ! -L "${current}" && -d "${current}" ]] || die "route state parent is not a canonical directory" 2
    owner="$(/usr/bin/stat -c '%u' -- "${current}")" || die "route state parent ownership cannot be checked" 2
    [[ "${owner}" == 0 ]] || die "route state parent must be root-owned" 2
    mode_hex="$(/usr/bin/stat -c '%f' -- "${current}")" || die "route state parent mode cannot be checked" 2
    if (( (0x${mode_hex} & 0x0012) != 0 )) && { [[ ! -d "${current}" || "${owner}" != 0 ]] || (( (0x${mode_hex} & 0x0200) == 0 )); }; then
      die "route state parent must not be group/world writable" 2
    fi
    [[ "${current}" == / ]] && break
    current="$(/usr/bin/dirname -- "${current}")"
  done
}

is_ipv4() {
  local value="$1" part count=0
  [[ "${value}" =~ ^[0-9]+(\.[0-9]+){3}$ ]] || return 1
  IFS=. read -r -a parts <<<"${value}"
  for part in "${parts[@]}"; do
    count=$((count + 1))
    [[ "${part}" =~ ^[0-9]{1,3}$ ]] && (( 10#${part} <= 255 )) || return 1
  done
  (( count == 4 ))
}

allowed_key() {
  case "$1" in
    POSTGRES_FAILOVER_TAILSCALE_SERVICE|POSTGRES_FAILOVER_ROUTE_SERVICE_PORT|POSTGRES_FAILOVER_ROUTE_SERVICE_DNS|\
    POSTGRES_FAILOVER_ROUTE_DB_NAME|POSTGRES_FAILOVER_ROUTE_PROBE_USER|POSTGRES_FAILOVER_ROUTE_PGPASS_FILE|\
    POSTGRES_FAILOVER_ROUTE_CA_FILE|POSTGRES_FAILOVER_ROUTE_TAILSCALE_PATH|\
    POSTGRES_FAILOVER_ROUTE_PSQL_PATH|POSTGRES_FAILOVER_ROUTE_IP_PATH|POSTGRES_FAILOVER_ROUTE_TAILSCALE_INTERFACE|\
    POSTGRES_FAILOVER_ROUTE_PROBE_TIMEOUT_SEC|POSTGRES_FAILOVER_ROUTE_STATE_FILE|POSTGRES_FAILOVER_ROUTE_JQ_PATH) return 0 ;;
    *) return 1 ;;
  esac
}

[[ "$(/usr/bin/id -u)" == 0 ]] || die "root is required" 2
secure_path "route config" "${CONFIG_FILE}" 0

POSTGRES_FAILOVER_TAILSCALE_SERVICE="svc:hololive-postgres"
POSTGRES_FAILOVER_ROUTE_SERVICE_PORT="5433"
POSTGRES_FAILOVER_ROUTE_SERVICE_DNS=""
POSTGRES_FAILOVER_ROUTE_DB_NAME="hololive"
POSTGRES_FAILOVER_ROUTE_PROBE_USER="hololive_replicator"
POSTGRES_FAILOVER_ROUTE_PGPASS_FILE="/etc/stack-secrets/hololive-bot/postgres-failover/route.pgpass"
POSTGRES_FAILOVER_ROUTE_CA_FILE="/etc/stack-secrets/hololive-bot/certs/postgres-ca.pem"
POSTGRES_FAILOVER_ROUTE_TAILSCALE_PATH="/usr/bin/tailscale"
POSTGRES_FAILOVER_ROUTE_PSQL_PATH="/usr/lib/postgresql/18/bin/psql"
POSTGRES_FAILOVER_ROUTE_IP_PATH="/usr/bin/ip"
POSTGRES_FAILOVER_ROUTE_TAILSCALE_INTERFACE="tailscale0"
POSTGRES_FAILOVER_ROUTE_PROBE_TIMEOUT_SEC="5"
POSTGRES_FAILOVER_ROUTE_STATE_FILE="/var/lib/hololive-postgres-route/route.state"
POSTGRES_FAILOVER_ROUTE_JQ_PATH="/usr/bin/jq"

declare -A seen=()
while IFS= read -r raw || [[ -n "${raw}" ]]; do
  line="${raw%$'\r'}"
  [[ "${line}" =~ ^[[:space:]]*$ || "${line}" =~ ^[[:space:]]*# ]] && continue
  [[ "${line}" =~ ^([A-Z][A-Z0-9_]*)=(.*)$ ]] || die "invalid route config line" 2
  key="${BASH_REMATCH[1]}"
  value="${BASH_REMATCH[2]}"
  allowed_key "${key}" || die "unsupported route config key: ${key}" 2
  [[ -z "${seen[${key}]:-}" ]] || die "duplicate route config key: ${key}" 2
  seen["${key}"]=1
  if [[ "${value}" =~ ^\'(.*)\'$ || "${value}" =~ ^\"(.*)\"$ ]]; then
    value="${BASH_REMATCH[1]}"
  fi
  [[ "${value}" =~ ^[A-Za-z0-9_./:@,+-]*$ ]] || die "invalid route config value: ${key}" 2
  printf -v "${key}" '%s' "${value}"
done <"${CONFIG_FILE}"

SERVICE="${POSTGRES_FAILOVER_TAILSCALE_SERVICE}"
SERVICE_PORT="${POSTGRES_FAILOVER_ROUTE_SERVICE_PORT}"
SERVICE_DNS="${POSTGRES_FAILOVER_ROUTE_SERVICE_DNS}"
DB_NAME="${POSTGRES_FAILOVER_ROUTE_DB_NAME}"
PROBE_USER="${POSTGRES_FAILOVER_ROUTE_PROBE_USER}"
PGPASS_FILE="${POSTGRES_FAILOVER_ROUTE_PGPASS_FILE}"
CA_FILE="${POSTGRES_FAILOVER_ROUTE_CA_FILE}"
TAILSCALE_PATH="${POSTGRES_FAILOVER_ROUTE_TAILSCALE_PATH}"
PSQL_PATH="${POSTGRES_FAILOVER_ROUTE_PSQL_PATH}"
IP_PATH="${POSTGRES_FAILOVER_ROUTE_IP_PATH}"
TAILSCALE_INTERFACE="${POSTGRES_FAILOVER_ROUTE_TAILSCALE_INTERFACE}"
PROBE_TIMEOUT_SEC="${POSTGRES_FAILOVER_ROUTE_PROBE_TIMEOUT_SEC}"
STATE_FILE="${POSTGRES_FAILOVER_ROUTE_STATE_FILE}"
JQ_PATH="${POSTGRES_FAILOVER_ROUTE_JQ_PATH}"

[[ "${SERVICE}" == svc:hololive-postgres ]] || die "unexpected Tailscale service" 2
if [[ -n "${TAILSCALE_SERVICE_ARGUMENT}" ]]; then
  is_tailscale_service "${TAILSCALE_SERVICE_ARGUMENT}" || die "invalid Tailscale service argument" 2
  [[ "${TAILSCALE_SERVICE_ARGUMENT}" == "${SERVICE}" ]] || die "Tailscale service argument does not match route config" 2
fi
if [[ "${SERVICE_PORT}" =~ ^[0-9]{1,5}$ ]]; then
  (( 10#${SERVICE_PORT} > 0 && 10#${SERVICE_PORT} <= 65535 )) || die "invalid Tailscale service port" 2
else
  die "invalid Tailscale service port" 2
fi
[[ -n "${SERVICE_DNS}" && "${SERVICE_DNS}" =~ ^[A-Za-z0-9.-]+$ && "${SERVICE_DNS}" != .* && "${SERVICE_DNS}" != *. ]] || die "invalid Tailscale service DNS name" 2
[[ -n "${DB_NAME}" && "${DB_NAME}" =~ ^[A-Za-z0-9_.-]+$ ]] || die "invalid database name" 2
[[ -n "${PROBE_USER}" && "${PROBE_USER}" =~ ^[A-Za-z0-9_.-]+$ ]] || die "invalid probe user" 2
is_host "${OLD_PRIMARY_HOST}" || die "invalid old primary host" 2
is_port "${OLD_PRIMARY_PORT}" || die "invalid old primary port" 2
is_host "${NEW_PRIMARY_HOST}" || die "invalid new primary host" 2
is_port "${NEW_PRIMARY_PORT}" || die "invalid new primary port" 2
is_token "${FENCE_TOKEN}" || die "invalid fence token" 2
is_path "${PGPASS_FILE}" || die "invalid pgpass path" 2
is_path "${CA_FILE}" || die "invalid PostgreSQL CA path" 2
is_path "${TAILSCALE_PATH}" || die "invalid tailscale path" 2
is_path "${PSQL_PATH}" || die "invalid psql path" 2
is_path "${IP_PATH}" || die "invalid ip path" 2
is_path "${JQ_PATH}" || die "invalid jq path" 2
[[ "${TAILSCALE_INTERFACE}" =~ ^[A-Za-z0-9._-]+$ ]] || die "invalid Tailscale interface" 2
if [[ "${PROBE_TIMEOUT_SEC}" =~ ^[1-9][0-9]{0,2}$ ]]; then
  (( 10#${PROBE_TIMEOUT_SEC} <= 300 )) || die "invalid probe timeout" 2
else
  die "invalid probe timeout" 2
fi
secure_path "pgpass" "${PGPASS_FILE}" 1
secure_path "PostgreSQL CA" "${CA_FILE}" 0
secure_path "tailscale executable" "${TAILSCALE_PATH}" 0
secure_path "psql executable" "${PSQL_PATH}" 0
secure_path "ip executable" "${IP_PATH}" 0
secure_path "jq executable" "${JQ_PATH}" 0
secure_optional_state_path "${STATE_FILE}"

local_ip_output="$("${IP_PATH}" -o -4 addr show dev "${TAILSCALE_INTERFACE}")" || die "could not inspect local Tailscale IP" 1
local_tailscale_ip=0
while IFS= read -r ip_line; do
  [[ -n "${ip_line}" ]] || continue
  read -r _ _ _ cidr _ <<<"${ip_line}"
  local_ip="${cidr%%/*}"
  if is_ipv4 "${local_ip}" && [[ "${local_ip}" == "${NEW_PRIMARY_HOST}" ]]; then
    local_tailscale_ip=1
    break
  fi
done <<<"${local_ip_output}"
(( local_tailscale_ip == 1 )) || die "new primary host is not a local Tailscale IP" 1

TARGET="tcp://${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}"
ENDPOINT="tcp:${SERVICE_PORT}"
"${TAILSCALE_PATH}" serve --yes "--service=${SERVICE}" "--tcp=${SERVICE_PORT}" "${TARGET}" >/dev/null || die "Tailscale service configuration failed" 1

config_json="$("${TAILSCALE_PATH}" serve get-config --all 2>/dev/null)" || die "Tailscale service configuration could not be read" 1
jq_filter="(.services[\$service].endpoints[\$endpoint] == \$target) and ((.services[\$service] | has(\"advertised\") | not) or (.services[\$service].advertised == true))"
"${JQ_PATH}" -e --arg service "${SERVICE}" --arg endpoint "${ENDPOINT}" --arg target "${TARGET}" \
  "${jq_filter}" \
  <<<"${config_json}" >/dev/null || die "Tailscale service advertisement does not match" 1

probe_output="$(PGPASSFILE="${PGPASS_FILE}" PGSSLROOTCERT="${CA_FILE}" PGSSLMODE=verify-full PGCONNECT_TIMEOUT="${PROBE_TIMEOUT_SEC}" \
  timeout "${PROBE_TIMEOUT_SEC}" "${PSQL_PATH}" -X -v ON_ERROR_STOP=1 -At \
  -h "${SERVICE_DNS}" -p "${SERVICE_PORT}" -U "${PROBE_USER}" -d "${DB_NAME}" \
  -c "SELECT CASE WHEN pg_is_in_recovery() THEN 't' ELSE 'f' END || '|' || current_setting('transaction_read_only');" 2>/dev/null)" \
  || die "Tailscale service PostgreSQL probe failed" 1
[[ "${probe_output}" == f\|off ]] || die "Tailscale service PostgreSQL probe was not writable" 1

state_tmp="${STATE_FILE}.tmp.$$"
umask 077
printf '%s\n' "${OLD_PRIMARY_HOST}|${OLD_PRIMARY_PORT}|${NEW_PRIMARY_HOST}|${NEW_PRIMARY_PORT}|${FENCE_TOKEN}" >"${state_tmp}" || die "route state write failed" 1
chmod 0600 "${state_tmp}" || { rm -f -- "${state_tmp}"; die "route state mode failed" 1; }
sync -f "${state_tmp}" || { rm -f -- "${state_tmp}"; die "route state sync failed" 1; }
mv -f -- "${state_tmp}" "${STATE_FILE}" || { rm -f -- "${state_tmp}"; die "route state publish failed" 1; }
sync -f "${STATE_FILE}" || die "route state publish sync failed" 1
sync -f "$(/usr/bin/dirname -- "${STATE_FILE}")" || die "route state directory sync failed" 1

printf 'ROUTED|%s:%s|%s\n' "${NEW_PRIMARY_HOST}" "${NEW_PRIMARY_PORT}" "${FENCE_TOKEN}"
