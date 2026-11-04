#!/usr/bin/env bash
# 제한된 sudo·SSH 명령으로 이전 primary에서 실행한다.
# 영속 격리 표시로 재부팅·의존 기동에서 PostgreSQL unit을 막고, 소비자는 유지한 채 PostgreSQL만 중지한다.

set -euo pipefail

REQUEST_ID="${1:-}"
EXPECTED_PRIMARY_HOST="${2:-}"
NEW_PRIMARY_HOST="${3:-}"
NEW_PRIMARY_PORT="${4:-}"
TAILSCALE_SERVICE="${5:-}"
POSTGRES_UNIT=holo-postgres.service
TAILSCALE_PATH="${POSTGRES_PRIMARY_FENCE_TAILSCALE_PATH:-/usr/bin/tailscale}"
PODMAN_PATH="${POSTGRES_PRIMARY_FENCE_PODMAN_PATH:-/usr/bin/podman}"
POSTGRES_CONTAINER=holo-postgres
POSTGRES_DATA_VOLUME=hololive-bot_holo-pg-data
STATE_DIR="${POSTGRES_PRIMARY_FENCE_STATE_DIR:-/var/lib/hololive-postgres-fence}"
ALLOW_TEST_STATE_DIR="${POSTGRES_PRIMARY_FENCE_ALLOW_TEST_STATE_DIR:-0}"
ALLOW_NON_ROOT_TEST="${POSTGRES_PRIMARY_FENCE_ALLOW_NON_ROOT_FOR_TEST:-0}"
FENCED_MARKER="${STATE_DIR}/fenced"
INTENT_MARKER="${STATE_DIR}/fence.intent"
LOCK_FILE="${STATE_DIR}/transition.lock"
NOW="${POSTGRES_PRIMARY_FENCE_NOW:-$(/usr/bin/date +%s)}"

is_token() { [[ "$1" =~ ^[A-Za-z0-9._:-]{8,128}$ ]]; }
is_host() { [[ "$1" =~ ^[A-Za-z0-9._:-]+$ ]]; }
is_tailscale_service() { [[ "$1" =~ ^svc:[a-z0-9][a-z0-9-]{0,62}$ ]]; }

trusted_executable() {
  local label="$1" path="$2" current owner mode_hex real
  [[ "${path}" == /* ]] || { echo "${label} must be absolute" >&2; return 1; }
  real="$(realpath -e -- "${path}")" || { echo "${label} is missing: ${path}" >&2; return 1; }
  [[ "${real}" == "${path}" && -f "${path}" && ! -L "${path}" && -x "${path}" ]] || {
    echo "${label} must be a canonical executable file" >&2
    return 1
  }
  current="${path}"
  while :; do
    [[ ! -L "${current}" && -e "${current}" ]] || {
      echo "${label} path contains a symlink or missing component: ${current}" >&2
      return 1
    }
    if [[ "${current}" == "/tmp" && "${ALLOW_TEST_STATE_DIR}" == "1" ]]; then
      break
    fi
    owner="$(stat -c '%u' -- "${current}")" || { echo "cannot stat ${label}: ${current}" >&2; return 1; }
    [[ "${owner}" == "0" || ( "${ALLOW_NON_ROOT_TEST}" == "1" && "${owner}" == "${CURRENT_UID}" ) ]] || {
      echo "${label} path must be root-owned: ${current}" >&2
      return 1
    }
    mode_hex="$(stat -c '%f' -- "${current}")" || { echo "cannot stat ${label}: ${current}" >&2; return 1; }
    (( (0x${mode_hex} & 0x0012) == 0 )) || {
      echo "${label} path must not be group/world writable: ${current}" >&2
      return 1
    }
    [[ "${current}" == "/" ]] && break
    current="$(dirname -- "${current}")"
  done
}

CURRENT_UID="$(/usr/bin/id -u)"
[[ "${ALLOW_NON_ROOT_TEST}" == "0" || "${ALLOW_NON_ROOT_TEST}" == "1" ]] || { echo "invalid non-root test flag" >&2; exit 2; }
if [[ "${ALLOW_NON_ROOT_TEST}" == "1" && ( "${CURRENT_UID}" == "0" || "${STATE_DIR}" != /tmp/* ) ]]; then
  echo "non-root test mode requires a non-root uid and /tmp state" >&2
  exit 2
fi
[[ "${CURRENT_UID}" == "0" || "${ALLOW_NON_ROOT_TEST}" == "1" ]] || { echo "primary fence requires root" >&2; exit 1; }
[[ "$#" -eq 5 ]] || { echo "request id, primary hosts, port, and Tailscale Service are required" >&2; exit 2; }
is_token "${REQUEST_ID}" || { echo "invalid request id" >&2; exit 2; }
is_host "${EXPECTED_PRIMARY_HOST}" || { echo "invalid expected primary host" >&2; exit 2; }
is_host "${NEW_PRIMARY_HOST}" || { echo "invalid new primary host" >&2; exit 2; }
if [[ ! "${NEW_PRIMARY_PORT}" =~ ^[0-9]+$ || ${#NEW_PRIMARY_PORT} -gt 5 ]] \
  || (( 10#${NEW_PRIMARY_PORT} <= 0 || 10#${NEW_PRIMARY_PORT} > 65535 )); then
  echo "invalid new primary port" >&2
  exit 2
fi
NEW_PRIMARY_PORT=$((10#${NEW_PRIMARY_PORT}))
is_tailscale_service "${TAILSCALE_SERVICE}" || { echo "invalid Tailscale Service" >&2; exit 2; }
[[ "${TAILSCALE_SERVICE}" == "svc:hololive-postgres" ]] || { echo "unexpected Tailscale Service" >&2; exit 2; }
[[ "${NOW}" =~ ^[0-9]+$ ]] || { echo "invalid timestamp" >&2; exit 2; }
[[ "${ALLOW_TEST_STATE_DIR}" == "0" || "${ALLOW_TEST_STATE_DIR}" == "1" ]] || { echo "invalid test state flag" >&2; exit 2; }
if [[ "${ALLOW_TEST_STATE_DIR}" == "1" && "${STATE_DIR}" != /tmp/* ]]; then
  echo "test fence state must be below /tmp" >&2
  exit 2
fi
if [[ "${ALLOW_TEST_STATE_DIR}" == "0" ]]; then
  PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
  export PATH
fi
[[ "${STATE_DIR}" == "/var/lib/hololive-postgres-fence" || "${ALLOW_TEST_STATE_DIR}" == "1" ]] || {
  echo "refusing noncanonical fence state directory" >&2
  exit 2
}
[[ "${STATE_DIR}" == /* ]] || { echo "state dir must be absolute" >&2; exit 2; }
trusted_executable "Tailscale binary" "${TAILSCALE_PATH}" || exit 1
trusted_executable "Podman binary" "${PODMAN_PATH}" || exit 1

# 잘못 라우팅된 SSH가 다른 호스트를 격리하지 않도록 요청 주소가 이 호스트에 있는지 확인한다.
if ! ip -o addr show scope global | awk '{print $4}' | cut -d/ -f1 | grep -Fxq -- "${EXPECTED_PRIMARY_HOST}"; then
  echo "expected primary address is not configured on this host" >&2
  exit 1
fi

install -d -m 0700 -- "${STATE_DIR}"
state_real="$(realpath -e -- "${STATE_DIR}")"
state_owner="$(stat -c '%u' -- "${STATE_DIR}")"
[[ "${state_real}" == "${STATE_DIR}" ]] || { echo "fence state directory must be canonical" >&2; exit 1; }
[[ "${state_owner}" == "0" || ( "${ALLOW_NON_ROOT_TEST}" == "1" && "${state_owner}" == "${CURRENT_UID}" ) ]] || {
  echo "fence state directory must be root-owned" >&2
  exit 1
}
state_mode_hex="$(stat -c '%f' -- "${STATE_DIR}")"
(( (0x${state_mode_hex} & 0x0012) == 0 )) || { echo "fence state directory is writable by group/other" >&2; exit 1; }

exec 9>"${LOCK_FILE}"
flock -x 9

marker_value() {
  local file="$1" key="$2"
  awk -F= -v key="${key}" '$1 == key {sub(/^[^=]*=/, ""); print; exit}' "${file}" 2>/dev/null || true
}

atomic_write() {
  local target="$1"
  local tmp="${target}.tmp.$$"
  umask 077
  cat >"${tmp}"
  chmod 0600 "${tmp}"
  sync -f "${tmp}"
  mv -f -- "${tmp}" "${target}"
  sync -f "${target}"
  sync -f "${STATE_DIR}"
}

# 재부팅 후에도 격리가 유지되어야 성공이다. 표식 조건이 없는 unit은 Wants= 의존·부팅에서 다시
# 기동되므로 거부한다. Restart=always는 명시적 stop 뒤에는 동작하지 않고, 이후 명시적 start와 의존 start job은
# 조건이 막는다. 자동 재시작은 조건을 다시 평가하지 않으므로 표식을 쓴 뒤 반드시 명시적으로 멈춘다.
need_daemon_reload="$(systemctl show "${POSTGRES_UNIT}" -p NeedDaemonReload --value)" || {
  echo "cannot inspect reload state for ${POSTGRES_UNIT}" >&2
  exit 1
}
[[ "${need_daemon_reload}" == "no" ]] || {
  echo "${POSTGRES_UNIT} requires daemon-reload before fencing" >&2
  exit 1
}
unit_text="$(systemctl cat "${POSTGRES_UNIT}")" || { echo "cannot inspect ${POSTGRES_UNIT}" >&2; exit 1; }
for condition in \
  'ConditionPathExists=!/var/lib/hololive-postgres-fence/fence.intent' \
  'ConditionPathExists=!/var/lib/hololive-postgres-fence/fenced'; do
  grep -Fqx -- "${condition}" <<<"${unit_text}" || {
    echo "${POSTGRES_UNIT} is missing persistent fence condition: ${condition}" >&2
    exit 1
  }
done

fence_token="${REQUEST_ID}"
if [[ -r "${FENCED_MARKER}" ]]; then
  existing_state="$(marker_value "${FENCED_MARKER}" state)"
  existing_host="$(marker_value "${FENCED_MARKER}" primary_host)"
  existing_token="$(marker_value "${FENCED_MARKER}" fence_token)"
  if [[ -z "${existing_token}" ]]; then
    existing_token="$(marker_value "${FENCED_MARKER}" request_id)"
  fi
  existing_new_primary="$(marker_value "${FENCED_MARKER}" new_primary)"
  existing_fenced_at="$(marker_value "${FENCED_MARKER}" fenced_at)"
  if [[ "${existing_state}" != "fenced" || "${existing_host}" != "${EXPECTED_PRIMARY_HOST}" \
    || "${existing_new_primary}" != "${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}" ]] \
    || ! is_token "${existing_token}" || [[ ! "${existing_fenced_at}" =~ ^[0-9]+$ ]]; then
    echo "existing fence marker is invalid" >&2
    exit 1
  fi
  fence_token="${existing_token}"
fi

unit_state() {
  systemctl show "${POSTGRES_UNIT}" -p ActiveState --value 2>/dev/null || printf 'unknown\n'
}
ORIGINAL_STATE="$(unit_state)"
case "${ORIGINAL_STATE}" in
  active|inactive|failed) ;;
  *) echo "${POSTGRES_UNIT} is transitioning (${ORIGINAL_STATE}); retry after it settles" >&2; exit 1 ;;
esac

MUTATION_STARTED=0
SERVICE_ROUTE_MUTATED=0
restore_service_after_incomplete_fence() {
  local rc=$? rollback_ok=1
  trap - EXIT
  # PostgreSQL이 아직 원래대로 실행 중일 때만 되돌린다. 이미 멈췄다면 intent를 남겨 fail-closed로 둔다.
  if (( rc != 0 && MUTATION_STARTED == 1 )) && [[ ! -r "${FENCED_MARKER}" ]] \
    && [[ "${ORIGINAL_STATE}" == active && "$(unit_state)" == active ]]; then
    if (( SERVICE_ROUTE_MUTATED == 1 )) \
      && ! "${TAILSCALE_PATH}" serve advertise "${TAILSCALE_SERVICE}" >/dev/null 2>&1; then
      rollback_ok=0
    fi
    if (( rollback_ok == 1 )); then
      rm -f -- "${INTENT_MARKER}" || rollback_ok=0
      sync -f "${STATE_DIR}" || rollback_ok=0
    fi
    (( rollback_ok == 1 )) || echo "failed to restore the primary after incomplete fence" >&2
  fi
  exit "${rc}"
}
trap restore_service_after_incomplete_fence EXIT
# unit이 inactive·failed여도 같은 이름의 수동 container나 PGDATA volume을 쓰는 다른 container가 돌 수 있다.
# Docker 시절 fence처럼 container 실행 여부를 직접 확인하고, 조회가 실패하면 격리 성공으로 보지 않는다.
postgres_containers_stopped() {
  local filter rows name state
  for filter in "name=^${POSTGRES_CONTAINER}\$" "volume=${POSTGRES_DATA_VOLUME}"; do
    rows="$("${PODMAN_PATH}" ps -a --filter "${filter}" --format '{{.Names}} {{.State}}')" || {
      echo "cannot verify PostgreSQL containers (${filter})" >&2
      return 1
    }
    while read -r name state; do
      [[ -n "${name}" ]] || continue
      case "${state}" in
        exited|created|stopped|configured) ;;
        *) echo "PostgreSQL container ${name} is still ${state}" >&2; return 1 ;;
      esac
    done <<<"${rows}"
  done
}
durably_stop_postgres() {
  local state
  systemctl stop "${POSTGRES_UNIT}" >/dev/null || true
  state="$(unit_state)"
  [[ "${state}" == inactive || "${state}" == failed ]] || {
    echo "${POSTGRES_UNIT} is not durably stopped (${state})" >&2
    return 1
  }
  postgres_containers_stopped
}

if [[ ! -r "${FENCED_MARKER}" ]]; then
  if [[ -r "${INTENT_MARKER}" ]]; then
    intent_state="$(marker_value "${INTENT_MARKER}" state)"
    intent_host="$(marker_value "${INTENT_MARKER}" primary_host)"
    intent_primary="$(marker_value "${INTENT_MARKER}" new_primary)"
    intent_token="$(marker_value "${INTENT_MARKER}" fence_token)"
    [[ -n "${intent_token}" ]] || intent_token="$(marker_value "${INTENT_MARKER}" request_id)"
    if [[ "${intent_state}" != fencing || "${intent_host}" != "${EXPECTED_PRIMARY_HOST}" \
      || "${intent_primary}" != "${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}" ]] \
      || ! is_token "${intent_token}"; then
      echo "existing fence intent is invalid" >&2
      exit 1
    fi
    fence_token="${intent_token}"
  else
    atomic_write "${INTENT_MARKER}" <<EOF_INTENT
state=fencing
request_id=${REQUEST_ID}
fence_token=${fence_token}
primary_host=${EXPECTED_PRIMARY_HOST}
new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}
started_at=${NOW}
EOF_INTENT
  fi
  MUTATION_STARTED=1
fi

SERVICE_ROUTE_MUTATED=1
if ! "${TAILSCALE_PATH}" serve drain "${TAILSCALE_SERVICE}" >/dev/null 2>&1; then
  echo "cannot drain Tailscale Service: ${TAILSCALE_SERVICE}" >&2
  exit 1
fi

# 소비자는 Wants=로만 묶여 있어 함께 멈추지 않는다. 같은 접속 주소를 유지하며 라우팅 변경 뒤 재연결한다.
durably_stop_postgres || exit 1

if [[ ! -r "${FENCED_MARKER}" ]]; then
  atomic_write "${FENCED_MARKER}" <<EOF_FENCED
state=fenced
request_id=${REQUEST_ID}
fence_token=${fence_token}
primary_host=${EXPECTED_PRIMARY_HOST}
new_primary=${NEW_PRIMARY_HOST}:${NEW_PRIMARY_PORT}
fenced_at=${NOW}
EOF_FENCED
fi
rm -f -- "${INTENT_MARKER}"
sync -f "${STATE_DIR}"
printf 'FENCED|%s|%s:%s|%s|%s\n' \
  "${EXPECTED_PRIMARY_HOST}" "${NEW_PRIMARY_HOST}" "${NEW_PRIMARY_PORT}" "${REQUEST_ID}" "${fence_token}"
