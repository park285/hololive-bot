#!/usr/bin/env bash
# 원격 split-host app file log를 /logs/remote/<target>로 mirror하고 기본 대상은 /logs/<service>.log symlink로 노출합니다.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${REPO_ROOT:-$(cd "${SCRIPT_DIR}/../.." && pwd)}"
LOG_ROOT="${LOG_ROOT:-/logs}"
REMOTE_MIRROR_ROOT="${REMOTE_MIRROR_ROOT:-${LOG_ROOT}/remote}"
FORCE_MAIN_LOG_LINKS="${FORCE_MAIN_LOG_LINKS:-0}"

OSAKA_USER_HOST="${HOL_LOG_OSAKA_USER_HOST:-ubuntu@kapu-iris-osaka-1}"
OSAKA_SSH_KEY="${HOL_LOG_OSAKA_SSH_KEY:-${REPO_ROOT}/KR.key}"
OSAKA_REMOTE_LOG_DIR="${HOL_LOG_OSAKA_LOG_DIR:-/home/ubuntu/hololive-bot/logs}"
OSAKA_SERVICES="${HOL_LOG_OSAKA_SERVICES:-youtube-producer-a youtube-producer-b}"
OSAKA_EXPLICIT_SERVICES="${HOL_LOG_OSAKA_EXPLICIT_SERVICES:-${OSAKA_SERVICES}}"

usage() {
  cat <<'USAGE'
Usage:
  remote-sync-main-logs.sh once osaka
  remote-sync-main-logs.sh daemon osaka [--interval 30]
  remote-sync-main-logs.sh status osaka
  remote-sync-main-logs.sh query osaka <service> [--tail 500] [--grep pattern]
  remote-sync-main-logs.sh tail osaka <service>
  remote-sync-main-logs.sh docker-tail osaka <service> [--since 15m] [--tail 200]

USAGE
}

target_dir() {
  local target="$1"
  printf '%s/%s\n' "${REMOTE_MIRROR_ROOT}" "${target}"
}

target_services() {
  local target="$1"
  case "${target}" in
    osaka) printf '%s\n' "${OSAKA_SERVICES}" ;;
    *) echo "ERROR: unknown target: ${target}" >&2; exit 1 ;;
  esac
}

validate_target_service() {
  local target="$1"
  local service="$2"
  local candidate

  [[ "${target}" == "osaka" ]] || { echo "ERROR: unknown target: ${target}" >&2; exit 1; }
  for candidate in ${OSAKA_EXPLICIT_SERVICES}; do
    if [[ "${candidate}" == "${service}" ]]; then
      return 0
    fi
  done

  echo "ERROR: unknown service for ${target}: ${service}" >&2
  exit 1
}

is_default_target_service() {
  local target="$1"
  local service="$2"
  local candidate

  for candidate in $(target_services "${target}"); do
    if [[ "${candidate}" == "${service}" ]]; then
      return 0
    fi
  done
  return 1
}

osaka_container_for() {
  local service="$1"
  case "${service}" in
    youtube-producer-a) printf '%s\n' "hololive-youtube-producer-a" ;;
    youtube-producer-b) printf '%s\n' "hololive-youtube-producer-b" ;;
    *) echo "ERROR: unknown service for osaka: ${service}" >&2; exit 1 ;;
  esac
}

ssh_base() {
  if [[ -n "${OSAKA_SSH_KEY}" ]]; then
    printf 'ssh -i %q -o IdentitiesOnly=yes -o BatchMode=yes -o ConnectTimeout=10' "${OSAKA_SSH_KEY}"
  else
    printf 'ssh -o BatchMode=yes -o ConnectTimeout=10'
  fi
}

rsync_ssh_base() {
  if [[ -n "${OSAKA_SSH_KEY}" ]]; then
    printf 'ssh -i %s -o IdentitiesOnly=yes -o BatchMode=yes -o ConnectTimeout=10' "${OSAKA_SSH_KEY}"
  else
    printf 'ssh -o BatchMode=yes -o ConnectTimeout=10'
  fi
}

ensure_log_root() {
  mkdir -p "${LOG_ROOT}" "${REMOTE_MIRROR_ROOT}"
}

normalize_mirror_permissions() {
  local dst="$1"

  if getent group docker >/dev/null 2>&1; then
    chgrp -R docker "${dst}" || true
  fi
  find "${dst}" -type d -exec chmod 2750 {} +
  find "${dst}" -type f -exec chmod 0640 {} +
}

sync_once_osaka() {
  ensure_log_root

  local dst
  dst="$(target_dir osaka)"
  mkdir -p "${dst}"

  if ! command -v rsync >/dev/null 2>&1; then
    echo "ERROR: rsync is required" >&2
    exit 1
  fi

  rsync -az \
    --partial \
    --delete-delay \
    --chmod=F0640,D0750 \
    --include='*.log' \
    --include='archive/' \
    --include='archive/***' \
    --include='*.gz' \
    --exclude='*' \
    -e "$(rsync_ssh_base)" \
    "${OSAKA_USER_HOST}:${OSAKA_REMOTE_LOG_DIR}/" \
    "${dst}/"

  date -Is > "${dst}/.last_sync"
  normalize_mirror_permissions "${dst}"
  ensure_main_links osaka
  echo "synced: ${OSAKA_USER_HOST}:${OSAKA_REMOTE_LOG_DIR} -> ${dst}" >&2
}

ensure_main_links() {
  local target="$1"
  local dst service source_rel source_abs link backup

  dst="$(target_dir "${target}")"

  for service in $(target_services "${target}"); do
    source_rel="remote/${target}/${service}.log"
    source_abs="${LOG_ROOT}/${source_rel}"
    link="${LOG_ROOT}/${service}.log"

    if [[ ! -f "${source_abs}" ]]; then
      echo "WARN: remote mirrored file not found yet: ${source_abs}" >&2
      continue
    fi

    if [[ -e "${link}" && ! -L "${link}" ]]; then
      if [[ "${FORCE_MAIN_LOG_LINKS}" != "1" ]]; then
        echo "WARN: ${link} exists as a regular file; not replacing. Set FORCE_MAIN_LOG_LINKS=1 to backup and replace." >&2
        continue
      fi
      backup="${link}.local.$(date +%Y%m%d-%H%M%S)"
      mv "${link}" "${backup}"
      echo "backed up existing local log: ${link} -> ${backup}" >&2
    fi

    (
      cd "${LOG_ROOT}"
      ln -sfn "${source_rel}" "${service}.log"
    )
  done
}

status_osaka() {
  local dst service link source last_sync
  dst="$(target_dir osaka)"
  last_sync="never"
  [[ -f "${dst}/.last_sync" ]] && last_sync="$(cat "${dst}/.last_sync")"

  echo "LOG_ROOT=${LOG_ROOT}"
  echo "remote_target=osaka"
  echo "remote_source=${OSAKA_USER_HOST}:${OSAKA_REMOTE_LOG_DIR}"
  echo "mirror_dir=${dst}"
  echo "last_sync=${last_sync}"
  echo

  for service in ${OSAKA_SERVICES}; do
    link="${LOG_ROOT}/${service}.log"
    source="${dst}/${service}.log"

    if [[ -L "${link}" ]]; then
      echo "${service}: main=${link} -> $(readlink "${link}")"
    elif [[ -e "${link}" ]]; then
      echo "${service}: main=${link} exists but is not symlink"
    else
      echo "${service}: main=${link} missing"
    fi

    if [[ -f "${source}" ]]; then
      echo "  mirror=${source} size=$(stat -c%s "${source}" 2>/dev/null || echo 0) mtime=$(stat -c%y "${source}" 2>/dev/null || echo unknown)"
    else
      echo "  mirror=${source} missing"
    fi
  done
}

daemon_osaka() {
  local interval="${REMOTE_LOG_SYNC_INTERVAL:-30}"
  shift || true

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --interval)
        interval="${2:-30}"
        shift 2
        ;;
      *)
        echo "ERROR: unknown arg: $1" >&2
        exit 1
        ;;
    esac
  done

  while true; do
    sync_once_osaka || true
    sleep "${interval}"
  done
}

service_log_path() {
  local target="$1"
  local service="$2"
  local main_file mirror_file
  main_file="${LOG_ROOT}/${service}.log"
  mirror_file="$(target_dir "${target}")/${service}.log"

  if ! is_default_target_service "${target}" "${service}" && [[ -e "${mirror_file}" ]]; then
    printf '%s\n' "${mirror_file}"
  elif [[ -e "${main_file}" ]]; then
    printf '%s\n' "${main_file}"
  elif [[ -e "${mirror_file}" ]]; then
    printf '%s\n' "${mirror_file}"
  else
    printf '%s\n' "${main_file}"
  fi
}

query_service() {
  local target="$1"
  local service="$2"
  shift || true
  shift || true

  local tail_lines="500"
  local grep_pattern=""
  local file

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --tail|--limit)
        tail_lines="${2:-500}"
        shift 2
        ;;
      --grep)
        grep_pattern="${2:-}"
        shift 2
        ;;
      *)
        echo "ERROR: unknown arg: $1" >&2
        exit 1
        ;;
    esac
  done

  file="$(service_log_path "${target}" "${service}")"
  if [[ ! -e "${file}" ]]; then
    echo "ERROR: log file not found: ${file}" >&2
    exit 1
  fi

  if [[ -n "${grep_pattern}" ]]; then
    tail -n "${tail_lines}" "${file}" | grep -E -- "${grep_pattern}" || true
  else
    tail -n "${tail_lines}" "${file}"
  fi
}

tail_service() {
  local target="$1"
  local service="$2"
  local file
  file="$(service_log_path "${target}" "${service}")"

  if [[ ! -e "${file}" ]]; then
    echo "ERROR: log file not found: ${file}" >&2
    exit 1
  fi

  exec tail -F "${file}"
}

docker_tail_osaka() {
  local service="$1"
  shift || true

  local container
  local since="15m"
  local tail_lines="200"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --since)
        since="${2:-15m}"
        shift 2
        ;;
      --tail|--limit)
        tail_lines="${2:-200}"
        shift 2
        ;;
      *)
        echo "ERROR: unknown arg: $1" >&2
        exit 1
        ;;
    esac
  done

  if [[ ! "${tail_lines}" =~ ^[0-9]+$ ]]; then
    echo "ERROR: --tail must be a positive integer" >&2
    exit 1
  fi
  if [[ ! "${since}" =~ ^[A-Za-z0-9_.:+-]+$ ]]; then
    echo "ERROR: --since contains unsupported characters" >&2
    exit 1
  fi

  local ssh_cmd
  container="$(osaka_container_for "${service}")"
  ssh_cmd="$(ssh_base)"

  # shellcheck disable=SC2029
  ${ssh_cmd} "${OSAKA_USER_HOST}" "docker logs --timestamps --since '${since}' --tail '${tail_lines}' '${container}' 2>&1"
}

cmd="${1:-help}"
case "${cmd}" in
  once)
    target="${2:-}"
    [[ "${target}" == "osaka" ]] || { echo "ERROR: only target 'osaka' is currently configured" >&2; exit 1; }
    sync_once_osaka
    ;;
  daemon)
    target="${2:-}"
    [[ "${target}" == "osaka" ]] || { echo "ERROR: only target 'osaka' is currently configured" >&2; exit 1; }
    shift 2 || true
    daemon_osaka "$@"
    ;;
  status)
    target="${2:-}"
    [[ "${target}" == "osaka" ]] || { echo "ERROR: only target 'osaka' is currently configured" >&2; exit 1; }
    status_osaka
    ;;
  query)
    target="${2:-}"
    service="${3:-}"
    [[ "${target}" == "osaka" ]] || { echo "ERROR: only target 'osaka' is currently configured" >&2; exit 1; }
    [[ -n "${service}" ]] || { echo "ERROR: service is required" >&2; exit 1; }
    validate_target_service "${target}" "${service}"
    shift 3 || true
    query_service "${target}" "${service}" "$@"
    ;;
  tail)
    target="${2:-}"
    service="${3:-}"
    [[ "${target}" == "osaka" ]] || { echo "ERROR: only target 'osaka' is currently configured" >&2; exit 1; }
    [[ -n "${service}" ]] || { echo "ERROR: service is required" >&2; exit 1; }
    validate_target_service "${target}" "${service}"
    tail_service "${target}" "${service}"
    ;;
  docker-tail)
    target="${2:-}"
    service="${3:-}"
    [[ "${target}" == "osaka" ]] || { echo "ERROR: only target 'osaka' is currently configured" >&2; exit 1; }
    [[ -n "${service}" ]] || { echo "ERROR: service is required" >&2; exit 1; }
    validate_target_service "${target}" "${service}"
    shift 3 || true
    docker_tail_osaka "${service}" "$@"
    ;;
  help|-h|--help)
    usage
    ;;
  *)
    echo "ERROR: unknown command: ${cmd}" >&2
    usage
    exit 1
    ;;
esac
