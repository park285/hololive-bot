#!/usr/bin/env bash
# Reference fence backend for postgres-failover.sh.
# It is deliberately fail-closed: SSH must reach the old primary and the remote
# fence script must return the exact FENCED acknowledgement.

set -euo pipefail

PRIMARY_HOST="${POSTGRES_FAILOVER_PRIMARY_HOST:?POSTGRES_FAILOVER_PRIMARY_HOST is required}"
NEW_PRIMARY_HOST="${POSTGRES_FAILOVER_NEW_PRIMARY_HOST:?POSTGRES_FAILOVER_NEW_PRIMARY_HOST is required}"
NEW_PRIMARY_PORT="${POSTGRES_FAILOVER_NEW_PRIMARY_PORT:?POSTGRES_FAILOVER_NEW_PRIMARY_PORT is required}"
REQUEST_ID="${POSTGRES_FAILOVER_REQUEST_ID:?POSTGRES_FAILOVER_REQUEST_ID is required}"
SSH_TARGET="${POSTGRES_FAILOVER_SSH_TARGET:?POSTGRES_FAILOVER_SSH_TARGET is required}"
SSH_IDENTITY_FILE="${POSTGRES_FAILOVER_SSH_IDENTITY_FILE:?POSTGRES_FAILOVER_SSH_IDENTITY_FILE is required}"
SSH_KNOWN_HOSTS_FILE="${POSTGRES_FAILOVER_SSH_KNOWN_HOSTS_FILE:?POSTGRES_FAILOVER_SSH_KNOWN_HOSTS_FILE is required}"
SSH_HOST_KEY_ALIAS="${POSTGRES_FAILOVER_SSH_HOST_KEY_ALIAS:-${PRIMARY_HOST}}"
SSH_CONNECT_TIMEOUT_SEC="${POSTGRES_FAILOVER_SSH_CONNECT_TIMEOUT_SEC:-5}"
REMOTE_FENCE_SCRIPT="${POSTGRES_FAILOVER_REMOTE_FENCE_SCRIPT:-/usr/local/libexec/hololive-postgres-failover/postgres-primary-fence.sh}"
TAILSCALE_SERVICE="${POSTGRES_FAILOVER_TAILSCALE_SERVICE:?POSTGRES_FAILOVER_TAILSCALE_SERVICE is required}"

is_token() { [[ "$1" =~ ^[A-Za-z0-9._:-]{8,128}$ ]]; }
is_host() { [[ "$1" =~ ^[A-Za-z0-9._:-]+$ ]]; }

secure_file() {
  local label="$1" path="$2" private="$3" real owner mode_hex current file_group credential_root
  [[ "${path}" == /* ]] || { echo "${label} must be absolute" >&2; return 1; }
  real="$(realpath -e -- "${path}")" || { echo "${label} missing: ${path}" >&2; return 1; }
  [[ "${real}" == "${path}" && -f "${path}" ]] || { echo "${label} must be a canonical regular file" >&2; return 1; }
  current="${path}"
  while :; do
    [[ ! -L "${current}" && -e "${current}" ]] || { echo "${label} path contains a symlink or missing component: ${current}" >&2; return 1; }
    owner="$(stat -c '%u' -- "${current}")"
    [[ "${owner}" == "0" ]] || { echo "${label} path must be root-owned: ${current}" >&2; return 1; }
    mode_hex="$(stat -c '%f' -- "${current}")"
    (( (0x${mode_hex} & 0x0012) == 0 )) || { echo "${label} path must not be group/world writable: ${current}" >&2; return 1; }
    [[ "${current}" == "/" ]] && break
    current="$(dirname -- "${current}")"
  done
  if [[ "${private}" == "1" ]]; then
    mode_hex="$(stat -c '%f' -- "${path}")"
    if (( (0x${mode_hex} & 0x003f) != 0 )); then
      file_group="$(stat -c '%g' -- "${path}")" || return 1
      credential_root="$(dirname -- "$(dirname -- "${path}")")"
      [[ "${credential_root}" == "/run/credentials" && "${owner}" == "0" && "${file_group}" == "0" \
        && $((0x${mode_hex} & 0x01ff)) == $((8#0440)) ]] \
        || { echo "${label} must not grant group/world permissions" >&2; return 1; }
    fi
  fi
}

is_host "${PRIMARY_HOST}" || { echo "invalid primary host" >&2; exit 2; }
is_host "${NEW_PRIMARY_HOST}" || { echo "invalid new primary host" >&2; exit 2; }
if [[ ! "${NEW_PRIMARY_PORT}" =~ ^[0-9]+$ || ${#NEW_PRIMARY_PORT} -gt 5 ]] \
  || (( 10#${NEW_PRIMARY_PORT} <= 0 || 10#${NEW_PRIMARY_PORT} > 65535 )); then
  echo "invalid new primary port" >&2
  exit 2
fi
is_token "${REQUEST_ID}" || { echo "invalid request id" >&2; exit 2; }
[[ "${SSH_TARGET}" =~ ^[A-Za-z0-9._-]+@[A-Za-z0-9._:-]+$ ]] || { echo "invalid SSH target" >&2; exit 2; }
[[ "${SSH_HOST_KEY_ALIAS}" =~ ^[A-Za-z0-9._:-]+$ ]] || { echo "invalid SSH host key alias" >&2; exit 2; }
if [[ ! "${SSH_CONNECT_TIMEOUT_SEC}" =~ ^[1-9][0-9]*$ || ${#SSH_CONNECT_TIMEOUT_SEC} -gt 3 ]] \
  || (( 10#${SSH_CONNECT_TIMEOUT_SEC} > 300 )); then
  echo "invalid SSH timeout" >&2
  exit 2
fi
[[ "${REMOTE_FENCE_SCRIPT}" =~ ^/[A-Za-z0-9._/-]+$ \
  && "${REMOTE_FENCE_SCRIPT}" != *'/../'* \
  && "${REMOTE_FENCE_SCRIPT}" != *'/./'* \
  && "${REMOTE_FENCE_SCRIPT}" != *//* ]] || { echo "invalid remote fence script path" >&2; exit 2; }
[[ "${TAILSCALE_SERVICE}" =~ ^svc:[a-z0-9][a-z0-9-]{0,62}$ ]] \
  || { echo "invalid Tailscale service" >&2; exit 2; }
secure_file "SSH identity" "${SSH_IDENTITY_FILE}" 1
secure_file "SSH known_hosts" "${SSH_KNOWN_HOSTS_FILE}" 0

remote_command="/usr/bin/sudo -n /usr/bin/env bash ${REMOTE_FENCE_SCRIPT} ${REQUEST_ID} ${PRIMARY_HOST} ${NEW_PRIMARY_HOST} ${NEW_PRIMARY_PORT} ${TAILSCALE_SERVICE}"
exec ssh -F /dev/null \
  -i "${SSH_IDENTITY_FILE}" \
  -o IdentitiesOnly=yes \
  -o BatchMode=yes \
  -o ClearAllForwardings=yes \
  -o ForwardAgent=no \
  -o RequestTTY=no \
  -o StrictHostKeyChecking=yes \
  -o "UserKnownHostsFile=${SSH_KNOWN_HOSTS_FILE}" \
  -o "HostKeyAlias=${SSH_HOST_KEY_ALIAS}" \
  -o "ConnectTimeout=${SSH_CONNECT_TIMEOUT_SEC}" \
  -o ConnectionAttempts=1 \
  -- "${SSH_TARGET}" "${remote_command}"
