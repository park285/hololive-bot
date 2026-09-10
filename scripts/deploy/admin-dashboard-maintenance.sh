#!/usr/bin/env bash
set -euo pipefail
ADMIN_MAINT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "${ADMIN_MAINT_ROOT}/scripts/deploy/lib/public-bind-mounts.sh"
if [[ $# -ne 3 || ! "$1" =~ ^(fence|open)$ || ! "$2" =~ ^[0-9a-f]{64}$ || "$3" != /* ]]; then
  echo 'usage: admin-dashboard-maintenance.sh fence|open <ingress-container-id> <mounted-config-path>' >&2
  exit 2
fi
ADMIN_MAINT_ACTION="$1"
ADMIN_MAINT_CONTAINER="$2"
ADMIN_MAINT_CONFIG="$3"
[[ -f "${ADMIN_MAINT_CONFIG}" && ! -L "${ADMIN_MAINT_CONFIG}" ]]
[[ "$(stat -c '%u' -- "${ADMIN_MAINT_CONFIG}")" == "$(id -u)" ]]
ADMIN_MAINT_NAME="$(docker inspect --format '{{.Name}}' "${ADMIN_MAINT_CONTAINER}")"
ADMIN_MAINT_LABEL="$(docker inspect --format '{{index .Config.Labels "io.hololive.admin-fixture"}}' "${ADMIN_MAINT_CONTAINER}")"
if [[ "${ADMIN_MAINT_NAME}" != '/admin-dashboard-ingress' ]]; then
  [[ "${ADMIN_MAINT_LABEL}" =~ ^admin-lab-[a-z0-9-]+$ && "${ADMIN_MAINT_CONFIG}" == "/tmp/${ADMIN_MAINT_LABEL}/"* ]] || {
    echo 'maintenance target is not the administrator ingress or an owned fixture' >&2
    exit 1
  }
fi
ADMIN_MAINT_MOUNT="$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/etc/nginx/admin-dashboard-ingress.conf"}}{{.Source}}{{end}}{{end}}' "${ADMIN_MAINT_CONTAINER}")"
[[ "${ADMIN_MAINT_MOUNT}" == "${ADMIN_MAINT_CONFIG}" ]]
ADMIN_MAINT_BIND="$(sed -nE 's/^[[:space:]]*listen ([0-9.]+):30191;/\1/p' "${ADMIN_MAINT_CONFIG}")"
is_admin_ingress_ipv4 "${ADMIN_MAINT_BIND}"
ADMIN_MAINT_MARKER="${ADMIN_MAINT_CONFIG}.maintenance"
restore_maintenance_after_open_failure() {
  local compensated=unknown deadline
  trap - ERR
  set +e
  # T09의 실패 시 정비 유지: origin 개방 실패에만 한 번 닫기를 적용하고 업무 요청은 재생하지 않습니다.
  if [[ ! -e "${ADMIN_MAINT_MARKER}" && ! -L "${ADMIN_MAINT_MARKER}" ]] \
    && (umask 077; set -o noclobber; printf 'closed\n' > "${ADMIN_MAINT_MARKER}") \
    && HOLOLIVE_BOT_PORT_BIND_IP="${ADMIN_MAINT_BIND}" HOLOLIVE_INGRESS_CONF="${ADMIN_MAINT_CONFIG}" \
      prepare_admin_dashboard_ingress_bind_mount "${ADMIN_MAINT_ROOT}" \
    && docker exec "${ADMIN_MAINT_CONTAINER}" nginx -t -c /etc/nginx/admin-dashboard-ingress.conf \
    && docker kill --signal HUP "${ADMIN_MAINT_CONTAINER}" >/dev/null; then
    deadline=$((SECONDS + 5))
    while (( SECONDS < deadline )); do
      if [[ "$(curl --noproxy '*' --max-time 1 -s -o /dev/null -w '%{http_code}' "http://${ADMIN_MAINT_BIND}:30191/health")" == 503 ]]; then
        compensated=confirmed
        break
      fi
      sleep 0.1
    done
  fi
  printf 'administrator open failed; maintenance_compensation=%s; stop cutover\n' "${compensated}" >&2
  exit 1
}
if [[ "${ADMIN_MAINT_ACTION}" == fence ]]; then
  [[ ! -L "${ADMIN_MAINT_MARKER}" ]]
  if [[ -e "${ADMIN_MAINT_MARKER}" ]]; then
    [[ -f "${ADMIN_MAINT_MARKER}" && "$(stat -c '%u:%a' "${ADMIN_MAINT_MARKER}")" == "$(id -u):600" && "$(cat "${ADMIN_MAINT_MARKER}")" == closed ]]
  else
    (umask 077; set -o noclobber; printf 'closed\n' > "${ADMIN_MAINT_MARKER}")
  fi
else
  [[ -f "${ADMIN_MAINT_MARKER}" && ! -L "${ADMIN_MAINT_MARKER}" ]]
  [[ "$(stat -c '%u:%a' "${ADMIN_MAINT_MARKER}")" == "$(id -u):600" && "$(cat "${ADMIN_MAINT_MARKER}")" == closed ]]
  # artifact·generation·세션 폐기 검증은 cutover runbook의 open 선행 조건입니다.
  rm -- "${ADMIN_MAINT_MARKER}"
  trap restore_maintenance_after_open_failure ERR
fi
HOLOLIVE_BOT_PORT_BIND_IP="${ADMIN_MAINT_BIND}" HOLOLIVE_INGRESS_CONF="${ADMIN_MAINT_CONFIG}" \
  prepare_admin_dashboard_ingress_bind_mount "${ADMIN_MAINT_ROOT}"
docker exec "${ADMIN_MAINT_CONTAINER}" nginx -t -c /etc/nginx/admin-dashboard-ingress.conf
ADMIN_MAINT_MASTER="$(docker inspect --format '{{.State.Pid}}' "${ADMIN_MAINT_CONTAINER}")"
ADMIN_MAINT_WORKERS="$(docker top "${ADMIN_MAINT_CONTAINER}" -eo pid,ppid,comm | awk -v master="${ADMIN_MAINT_MASTER}" '$2 == master && $3 == "nginx" {print $1}')"
[[ -n "${ADMIN_MAINT_WORKERS}" ]]
ADMIN_MAINT_DEADLINE=$((SECONDS + 30))
docker kill --signal HUP "${ADMIN_MAINT_CONTAINER}" >/dev/null
ADMIN_MAINT_EXPECTED=503
[[ "${ADMIN_MAINT_ACTION}" != open ]] || ADMIN_MAINT_EXPECTED=200
while (( SECONDS < ADMIN_MAINT_DEADLINE )); do
  if [[ "${ADMIN_MAINT_ACTION}" == open ]]; then
    # C06: 새 연결의 200만으로는 기존 keep-alive의 정비 worker가 끝났다고 볼 수 없습니다.
    ADMIN_MAINT_CURRENT="$(docker top "${ADMIN_MAINT_CONTAINER}" -eo pid,ppid,comm | awk -v master="${ADMIN_MAINT_MASTER}" '$2 == master && $3 == "nginx" {print $1}')"
    ADMIN_MAINT_WAIT=false
    for worker in ${ADMIN_MAINT_WORKERS}; do
      if [[ " ${ADMIN_MAINT_CURRENT//$'\n'/ } " == *" ${worker} "* ]]; then
        ADMIN_MAINT_WAIT=true
      fi
    done
    if [[ "${ADMIN_MAINT_WAIT}" == true ]]; then
      sleep 0.1
      continue
    fi
  fi
  ADMIN_MAINT_STATUS="$(curl --noproxy '*' --max-time 2 -s -o /dev/null -w '%{http_code}' "http://${ADMIN_MAINT_BIND}:30191/health")" || ADMIN_MAINT_STATUS=unavailable
  if [[ "${ADMIN_MAINT_STATUS}" == "${ADMIN_MAINT_EXPECTED}" ]]; then
    [[ "$(curl --noproxy '*' --max-time 2 -s -o /dev/null -w '%{http_code}' "http://${ADMIN_MAINT_BIND}:30192/healthz")" == 200 ]]
    printf 'administrator_ingress=%s shortlink_health=200 container=%s\n' "${ADMIN_MAINT_STATUS}" "${ADMIN_MAINT_CONTAINER}"
    trap - ERR
    exit 0
  fi
  sleep 0.1
done
echo 'administrator ingress transition was not confirmed; preserve maintenance and inspect before continuing' >&2
if [[ "${ADMIN_MAINT_ACTION}" == open ]]; then
  restore_maintenance_after_open_failure
fi
exit 1
