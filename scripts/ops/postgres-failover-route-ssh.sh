#!/usr/bin/env bash

set -euo pipefail

OLD_PRIMARY_HOST="${POSTGRES_FAILOVER_OLD_PRIMARY_HOST:-${POSTGRES_FAILOVER_PRIMARY_HOST:-}}"
OLD_PRIMARY_PORT="${POSTGRES_FAILOVER_OLD_PRIMARY_PORT:-${POSTGRES_FAILOVER_PRIMARY_PORT:-}}"
NEW_PRIMARY_HOST="${POSTGRES_FAILOVER_NEW_PRIMARY_HOST:-}"
NEW_PRIMARY_PORT="${POSTGRES_FAILOVER_NEW_PRIMARY_PORT:-}"
FENCE_TOKEN="${POSTGRES_FAILOVER_FENCE_TOKEN:-}"
TAILSCALE_SERVICE="${POSTGRES_FAILOVER_TAILSCALE_SERVICE:-}"
ROUTE_SSH_TARGET="${POSTGRES_FAILOVER_ROUTE_SSH_TARGET:-}"
ROUTE_SSH_IDENTITY_FILE="${POSTGRES_FAILOVER_ROUTE_SSH_IDENTITY_FILE:-}"
ROUTE_SSH_KNOWN_HOSTS_FILE="${POSTGRES_FAILOVER_ROUTE_SSH_KNOWN_HOSTS_FILE:-}"
ROUTE_SSH_HOST_KEY_ALIAS="${POSTGRES_FAILOVER_ROUTE_SSH_HOST_KEY_ALIAS:-${NEW_PRIMARY_HOST}}"
ROUTE_SSH_CONNECT_TIMEOUT_SEC="${POSTGRES_FAILOVER_ROUTE_SSH_CONNECT_TIMEOUT_SEC:-5}"
ROUTE_REMOTE_SCRIPT="${POSTGRES_FAILOVER_ROUTE_REMOTE_SCRIPT:-/usr/local/libexec/hololive-postgres-failover/postgres-route-tailscale.sh}"
ROUTE_CONFIG_FILE="${POSTGRES_FAILOVER_ROUTE_CONFIG_FILE:-/etc/hololive-postgres-failover/route.env}"
ALLOW_NON_ROOT_TEST="${POSTGRES_FAILOVER_ROUTE_ALLOW_NON_ROOT_FOR_TEST:-0}"
CURRENT_UID="$(/usr/bin/id -u)"

die() {
  printf '[postgres-failover-route] %s\n' "$1" >&2
  exit "${2:-2}"
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

secure_file() {
  local label="$1" path="$2" private="$3" current real owner mode_hex file_group credential_root
  is_path "${path}" || die "${label} path is invalid"
  real="$(/usr/bin/realpath -e -- "${path}")" || die "${label} is missing"
  [[ "${real}" == "${path}" && -f "${path}" && ! -L "${path}" ]] || die "${label} must be a canonical regular file"
  current="${path}"
  while :; do
    [[ ! -L "${current}" && -e "${current}" ]] || die "${label} path contains a symlink or missing component"
    owner="$(/usr/bin/stat -c '%u' -- "${current}")" || die "${label} ownership cannot be checked"
    if [[ "${owner}" != 0 && ( "${ALLOW_NON_ROOT_TEST}" != 1 || "${CURRENT_UID}" == 0 || "${path}" != /tmp/* || "${owner}" != "${CURRENT_UID}" ) ]]; then
      die "${label} path must be root-owned"
    fi
    mode_hex="$(/usr/bin/stat -c '%f' -- "${current}")" || die "${label} mode cannot be checked"
    if (( (0x${mode_hex} & 0x0012) != 0 )) && { [[ ! -d "${current}" || "${owner}" != 0 ]] || (( (0x${mode_hex} & 0x0200) == 0 )); }; then
      die "${label} path must not be group/world writable"
    fi
    [[ "${current}" == / ]] && break
    current="$(/usr/bin/dirname -- "${current}")"
  done
  if [[ "${private}" == 1 ]]; then
    mode_hex="$(/usr/bin/stat -c '%f' -- "${path}")" || die "${label} mode cannot be checked"
    if (( (0x${mode_hex} & 0x003f) != 0 )); then
      file_group="$(/usr/bin/stat -c '%g' -- "${path}")" || die "${label} group cannot be checked"
      credential_root="$(/usr/bin/dirname -- "$(/usr/bin/dirname -- "${path}")")"
      [[ "${credential_root}" == "/run/credentials" && "${owner}" == 0 && "${file_group}" == 0 \
        && $((0x${mode_hex} & 0x01ff)) == $((8#0440)) ]] \
        || die "${label} must not grant group/world permissions"
    fi
  fi
}

is_host "${OLD_PRIMARY_HOST}" || die "invalid old primary host"
is_port "${OLD_PRIMARY_PORT}" || die "invalid old primary port"
is_host "${NEW_PRIMARY_HOST}" || die "invalid new primary host"
is_port "${NEW_PRIMARY_PORT}" || die "invalid new primary port"
is_token "${FENCE_TOKEN}" || die "invalid fence token"
is_tailscale_service "${TAILSCALE_SERVICE}" || die "invalid Tailscale service"
[[ "${ALLOW_NON_ROOT_TEST}" == 0 || "${ALLOW_NON_ROOT_TEST}" == 1 ]] || die "invalid route test mode"
if [[ "${ALLOW_NON_ROOT_TEST}" == 1 && ( "${CURRENT_UID}" == 0 || "${ROUTE_SSH_IDENTITY_FILE}" != /tmp/* || "${ROUTE_SSH_KNOWN_HOSTS_FILE}" != /tmp/* ) ]]; then
  die "non-root route test mode requires a non-root uid and /tmp credentials"
fi
[[ "${ROUTE_SSH_TARGET}" =~ ^[A-Za-z0-9._-]+@[A-Za-z0-9._:-]+$ ]] || die "invalid route SSH target"
is_host "${ROUTE_SSH_HOST_KEY_ALIAS}" || die "invalid route SSH host key alias"
if [[ "${ROUTE_SSH_CONNECT_TIMEOUT_SEC}" =~ ^[1-9][0-9]{0,2}$ ]]; then
  (( 10#${ROUTE_SSH_CONNECT_TIMEOUT_SEC} <= 300 )) || die "invalid route SSH timeout"
else
  die "invalid route SSH timeout"
fi
is_path "${ROUTE_REMOTE_SCRIPT}" || die "invalid remote route helper path"
is_path "${ROUTE_CONFIG_FILE}" || die "invalid route config path"
secure_file "route SSH identity" "${ROUTE_SSH_IDENTITY_FILE}" 1
secure_file "route SSH known_hosts" "${ROUTE_SSH_KNOWN_HOSTS_FILE}" 0

remote_command="/usr/bin/sudo -n /usr/bin/env bash ${ROUTE_REMOTE_SCRIPT} --config ${ROUTE_CONFIG_FILE} ${OLD_PRIMARY_HOST} ${OLD_PRIMARY_PORT} ${NEW_PRIMARY_HOST} ${NEW_PRIMARY_PORT} ${FENCE_TOKEN} ${TAILSCALE_SERVICE}"
if ! ack="$(ssh -F /dev/null \
  -i "${ROUTE_SSH_IDENTITY_FILE}" \
  -o IdentitiesOnly=yes \
  -o BatchMode=yes \
  -o ClearAllForwardings=yes \
  -o ForwardAgent=no \
  -o RequestTTY=no \
  -o StrictHostKeyChecking=yes \
  -o "UserKnownHostsFile=${ROUTE_SSH_KNOWN_HOSTS_FILE}" \
  -o "HostKeyAlias=${ROUTE_SSH_HOST_KEY_ALIAS}" \
  -o "ConnectTimeout=${ROUTE_SSH_CONNECT_TIMEOUT_SEC}" \
  -o ConnectionAttempts=1 \
  -- "${ROUTE_SSH_TARGET}" "${remote_command}")"; then
  die "route SSH helper failed" 1
fi

expected="ROUTED|${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}|${FENCE_TOKEN}"
[[ "${ack}" == "${expected}" ]] || die "invalid route helper acknowledgement" 1
printf '%s\n' "${ack}"
