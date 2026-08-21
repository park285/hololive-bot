#!/usr/bin/env bash
set -euo pipefail

SOURCE_ENV="${ADMIN_DASHBOARD_ENV_FILE:-/etc/stack-secrets/hololive-bot/admin-dashboard.env}"
COMPOSE_ENV="${COMPOSE_ENV_FILE:-/etc/stack-secrets/hololive-bot/compose.env}"
DEST_DIR="${ADMIN_DASHBOARD_SECRET_DIR:-/run/hololive-bot/admin-secrets}"

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  echo "[SECURITY] admin secret materialization must run as root" >&2
  exit 1
fi
if [[ -L "${SOURCE_ENV}" || ! -f "${SOURCE_ENV}" ]]; then
  echo "[SECURITY] admin secret source must be a regular non-symlink file: ${SOURCE_ENV}" >&2
  exit 1
fi
if [[ "$(stat -c '%u' -- "${SOURCE_ENV}")" -ne 0 ]]; then
  echo "[SECURITY] admin secret source must be root-owned: ${SOURCE_ENV}" >&2
  exit 1
fi
source_mode="$(printf '%04d' "$((10#$(stat -c '%a' -- "${SOURCE_ENV}")))")"
if (( (8#${source_mode} & 8#0077) != 0 )); then
  echo "[SECURITY] admin secret source must not be group/other accessible: ${SOURCE_ENV} mode=${source_mode}" >&2
  exit 1
fi

runtime_gid="${HOLOLIVE_RUNTIME_GID:-}"
if [[ -z "${runtime_gid}" && -r "${COMPOSE_ENV}" ]]; then
  runtime_gid="$(awk -F= '$1 == "HOLOLIVE_RUNTIME_GID" {print $2; exit}' "${COMPOSE_ENV}")"
fi
runtime_gid="${runtime_gid:-1002}"
case "${runtime_gid}" in
  ''|*[!0-9]*)
    echo "[SECURITY] HOLOLIVE_RUNTIME_GID must be numeric" >&2
    exit 1
    ;;
esac

parent="$(dirname -- "${DEST_DIR}")"
if [[ -L "${parent}" || -L "${DEST_DIR}" ]]; then
  echo "[SECURITY] admin secret destination must not contain a symlink endpoint: ${DEST_DIR}" >&2
  exit 1
fi
install -d -o root -g "${runtime_gid}" -m 0750 "${parent}" "${DEST_DIR}"

read_env_value() {
  local key="$1"
  awk -v want="${key}" '
    /^[[:space:]]*(#|$)/ { next }
    {
      eq = index($0, "=")
      if (eq == 0) next
      key = substr($0, 1, eq - 1)
      if (key == want) {
        count++
        value = substr($0, eq + 1)
      }
    }
    END {
      if (count > 1) exit 42
      if (count == 0) exit 43
      sub(/\r$/, "", value)
      if (value ~ /^".*"$/ || value ~ /^\047.*\047$/) {
        value = substr(value, 2, length(value) - 2)
      }
      printf "%s", value
    }
  ' "${SOURCE_ENV}"
}

write_secret() {
  local key="$1"
  local filename="$2"
  local required="$3"
  local value=""
  local status=0

  set +e
  value="$(read_env_value "${key}")"
  status=$?
  set -e
  if [[ ${status} -eq 42 ]]; then
    echo "[SECURITY] duplicate ${key} in ${SOURCE_ENV}" >&2
    exit 1
  fi
  if [[ ${status} -eq 43 ]]; then
    if [[ "${required}" == "required" ]]; then
      echo "[SECURITY] required ${key} missing from ${SOURCE_ENV}" >&2
      exit 1
    fi
    value=""
  elif [[ ${status} -ne 0 ]]; then
    echo "[SECURITY] failed reading ${key} from ${SOURCE_ENV}" >&2
    exit 1
  fi
  if [[ "${required}" == "required" && -z "${value}" ]]; then
    echo "[SECURITY] required ${key} is empty in ${SOURCE_ENV}" >&2
    exit 1
  fi

  local target="${DEST_DIR}/${filename}"
  local tmp="${target}.tmp.$$"
  umask 077
  printf '%s' "${value}" >"${tmp}"
  chown root:"${runtime_gid}" "${tmp}"
  chmod 0640 "${tmp}"
  mv -fT "${tmp}" "${target}"
}

write_secret ADMIN_PASS_HASH admin-pass-hash required
write_secret SESSION_SECRET session-secret required
write_secret VALKEY_URL valkey-url required
write_secret HOLO_BOT_API_KEY holo-bot-api-key optional

session_bytes="$(wc -c <"${DEST_DIR}/session-secret")"
if (( session_bytes < 32 )); then
  echo "[SECURITY] SESSION_SECRET must be at least 32 bytes; got ${session_bytes}" >&2
  exit 1
fi

# Remove any stale temporary files without ever printing secret contents.
find "${DEST_DIR}" -maxdepth 1 -type f -name '*.tmp.*' -delete
