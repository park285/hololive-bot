#!/usr/bin/env bash

set -euo pipefail

MODE="${1:---dry-run}"
case "${MODE}" in
  --dry-run|--apply) ;;
  *) printf 'Usage: %s [--dry-run|--apply]\n' "$0" >&2; exit 2 ;;
esac

PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH
unset BASH_ENV ENV LD_PRELOAD LD_LIBRARY_PATH

ENV_FILE="${POSTGRES_FAILOVER_ENV_FILE:-/etc/stack-secrets/hololive-bot/postgres-failover.env}"
CONTROLLER="${POSTGRES_FAILOVER_CONTROLLER:-/usr/local/libexec/hololive-postgres-failover/postgres-failover.sh}"
ALLOW_NON_ROOT_TEST="${POSTGRES_FAILOVER_LAUNCH_ALLOW_NON_ROOT_FOR_TEST:-0}"
SERVICE_USER="${POSTGRES_FAILOVER_SERVICE_USER:-hololive-pg-failover}"
CURRENT_UID="$(/usr/bin/id -u)"
CURRENT_GID="$(/usr/bin/id -g)"
CURRENT_USER="$(/usr/bin/id -un)"

[[ "${ALLOW_NON_ROOT_TEST}" == "0" || "${ALLOW_NON_ROOT_TEST}" == "1" ]] || { printf 'invalid launcher test flag\n' >&2; exit 2; }
if [[ "${ALLOW_NON_ROOT_TEST}" == "1" && ( "${CURRENT_UID}" == "0" || "${ENV_FILE}" != /tmp/* || "${CONTROLLER}" != /tmp/* ) ]]; then
  printf 'launcher test mode requires non-root uid and /tmp paths\n' >&2
  exit 2
fi
if [[ "${ALLOW_NON_ROOT_TEST}" == "0" && "${CURRENT_USER}" != "${SERVICE_USER}" ]]; then
  printf 'failover launcher must run as %s, got %s\n' "${SERVICE_USER}" "${CURRENT_USER}" >&2
  exit 1
fi

trusted_path() {
  local label="$1" path="$2" private="$3" real current owner mode_hex mode file_owner file_group credential_parent credential_root credential_copy
  [[ "${path}" == /* ]] || { printf '%s must be absolute\n' "${label}" >&2; return 1; }
  real="$(/usr/bin/realpath -e -- "${path}")" || { printf '%s is missing\n' "${label}" >&2; return 1; }
  [[ "${real}" == "${path}" && -f "${path}" && ! -L "${path}" ]] || { printf '%s must be a canonical regular file\n' "${label}" >&2; return 1; }
  current="${path}"
  while :; do
    owner="$(/usr/bin/stat -c '%u' -- "${current}")" || return 1
    if [[ "${owner}" != "0" \
      && ( "${owner}" != "${CURRENT_UID}" || ( "${ALLOW_NON_ROOT_TEST}" != "1" && "${path}" != /run/credentials/* ) ) ]]; then
      printf '%s path has an untrusted owner: %s\n' "${label}" "${current}" >&2
      return 1
    fi
    mode_hex="$(/usr/bin/stat -c '%f' -- "${current}")" || return 1
    mode=$((0x${mode_hex}))
    if (( (mode & 0x0012) != 0 )) && { [[ ! -d "${current}" || "${owner}" != "0" ]] || (( (mode & 0x0200) == 0 )); }; then
      printf '%s path is writable by group or other: %s\n' "${label}" "${current}" >&2
      return 1
    fi
    [[ "${current}" == "/" ]] && break
    current="$(/usr/bin/dirname -- "${current}")"
  done
  if [[ "${private}" == "1" ]]; then
    file_owner="$(/usr/bin/stat -c '%u' -- "${path}")" || return 1
    file_group="$(/usr/bin/stat -c '%g' -- "${path}")" || return 1
    mode_hex="$(/usr/bin/stat -c '%f' -- "${path}")" || return 1
    mode=$((0x${mode_hex} & 0x01ff))
    if (( (mode & 0x003f) != 0 )); then
      credential_parent="$(/usr/bin/dirname -- "${path}")"
      credential_root="$(/usr/bin/dirname -- "${credential_parent}")"
      credential_copy=0
      if [[ "${credential_root}" == "/run/credentials" && "${file_owner}" == "0" && "${file_group}" == "0" ]]; then
        credential_copy=1
      elif [[ "${ALLOW_NON_ROOT_TEST}" == "1" && "${credential_root}" == /tmp/*/run/credentials && "${file_owner}" == "${CURRENT_UID}" && "${file_group}" == "${CURRENT_GID}" ]]; then
        credential_copy=1
      fi
      if (( mode != 0x0120 || credential_copy != 1 )); then
        printf '%s must be private\n' "${label}" >&2
        return 1
      fi
    fi
  fi
}

allowed_key() {
  case "$1" in
    POSTGRES_FAILOVER_DB_NAME|POSTGRES_FAILOVER_PROBE_USER|POSTGRES_FAILOVER_LOCAL_HOST|POSTGRES_FAILOVER_LOCAL_PORT|\
    POSTGRES_FAILOVER_PRIMARY_HOST|POSTGRES_FAILOVER_PRIMARY_PORT|POSTGRES_FAILOVER_NEW_PRIMARY_HOST|POSTGRES_FAILOVER_NEW_PRIMARY_PORT|\
    POSTGRES_FAILOVER_FAILURE_THRESHOLD|\
    POSTGRES_FAILOVER_MIN_OUTAGE_SEC|POSTGRES_FAILOVER_MAX_LAST_HEALTHY_AGE_SEC|POSTGRES_FAILOVER_MAX_KNOWN_LAG_BYTES|\
    POSTGRES_FAILOVER_PROBE_TIMEOUT_SEC|POSTGRES_FAILOVER_PROMOTE_TIMEOUT_SEC|POSTGRES_FAILOVER_FENCE_HOOK_TIMEOUT_SEC|\
    POSTGRES_FAILOVER_ROUTE_HOOK_TIMEOUT_SEC|POSTGRES_FAILOVER_REQUIRE_ROUTE_HOOK|POSTGRES_FAILOVER_FENCE_COMMAND|\
    POSTGRES_FAILOVER_ROUTE_COMMAND|POSTGRES_FAILOVER_SSH_TARGET|POSTGRES_FAILOVER_SSH_HOST_KEY_ALIAS|POSTGRES_FAILOVER_SSH_CONNECT_TIMEOUT_SEC|\
    POSTGRES_FAILOVER_REMOTE_FENCE_SCRIPT|POSTGRES_FAILOVER_ROUTE_SSH_TARGET|POSTGRES_FAILOVER_ROUTE_SSH_HOST_KEY_ALIAS|\
    POSTGRES_FAILOVER_ROUTE_SSH_CONNECT_TIMEOUT_SEC|POSTGRES_FAILOVER_ROUTE_REMOTE_SCRIPT|POSTGRES_FAILOVER_TAILSCALE_SERVICE|\
    POSTGRES_FAILOVER_ROUTE_CONFIG_FILE) return 0 ;;
    *) return 1 ;;
  esac
}

trusted_path "failover environment" "${ENV_FILE}" 1
trusted_path "failover controller" "${CONTROLLER}" 0

declare -A seen=()
while IFS= read -r raw || [[ -n "${raw}" ]]; do
  line="${raw%$'\r'}"
  [[ "${line}" =~ ^[[:space:]]*$ || "${line}" =~ ^[[:space:]]*# ]] && continue
  [[ "${line}" =~ ^([A-Z][A-Z0-9_]*)=(.*)$ ]] || { printf 'invalid failover environment line\n' >&2; exit 2; }
  key="${BASH_REMATCH[1]}"
  value="${BASH_REMATCH[2]}"
  allowed_key "${key}" || { printf 'unsupported failover environment key: %s\n' "${key}" >&2; exit 2; }
  [[ -z "${seen[${key}]:-}" ]] || { printf 'duplicate failover environment key: %s\n' "${key}" >&2; exit 2; }
  seen["${key}"]=1
  if [[ "${value}" =~ ^\'(.*)\'$ || "${value}" =~ ^\"(.*)\"$ ]]; then
    value="${BASH_REMATCH[1]}"
  fi
  [[ "${value}" =~ ^[A-Za-z0-9_./:@,+-]*$ ]] || { printf 'invalid failover environment value: %s\n' "${key}" >&2; exit 2; }
  printf -v "${key}" '%s' "${value}"
  export "${key?}"
done <"${ENV_FILE}"

if [[ "${MODE}" == "--apply" ]]; then
  [[ -n "${POSTGRES_FAILOVER_ROUTE_SSH_IDENTITY_FILE:-}" ]] || {
    printf 'route SSH identity credential is required for apply\n' >&2
    exit 1
  }
  [[ -n "${POSTGRES_FAILOVER_ROUTE_SSH_KNOWN_HOSTS_FILE:-}" ]] || {
    printf 'route SSH known_hosts credential is required for apply\n' >&2
    exit 1
  }
  trusted_path "route SSH identity" "${POSTGRES_FAILOVER_ROUTE_SSH_IDENTITY_FILE}" 1
  trusted_path "route SSH known_hosts" "${POSTGRES_FAILOVER_ROUTE_SSH_KNOWN_HOSTS_FILE}" 0
fi

exec /usr/bin/bash "${CONTROLLER}" "${MODE}"
