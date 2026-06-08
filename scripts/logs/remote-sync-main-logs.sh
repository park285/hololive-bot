#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${REPO_ROOT:-$(cd "${SCRIPT_DIR}/../.." && pwd)}"
LOG_ROOT="${LOG_ROOT:-${REPO_ROOT}/logs}"
REMOTE_MIRROR_ROOT="${REMOTE_MIRROR_ROOT:-${LOG_ROOT}/remote}"
FORCE_MAIN_LOG_LINKS="${FORCE_MAIN_LOG_LINKS:-0}"
OSAKA_USER_HOST="${HOL_LOG_OSAKA_USER_HOST:-ubuntu@kapu-iris-osaka-1}"
OSAKA_SSH_KEY="${HOL_LOG_OSAKA_SSH_KEY:-${REPO_ROOT}/KR.key}"
OSAKA_HOST_KEY_ALIAS="${HOL_LOG_OSAKA_HOST_KEY_ALIAS:-100.100.1.7}"
OSAKA_REMOTE_LOG_DIR="${HOL_LOG_OSAKA_LOG_DIR:-/home/ubuntu/hololive-bot/logs}"
OSAKA_SERVICES="${HOL_LOG_OSAKA_SERVICES:-youtube-producer}"
OSAKA_DOCKER_SERVICES="${HOL_LOG_OSAKA_DOCKER_SERVICES:-youtube-producer youtube-producer-a}"
SEOUL_USER_HOST="${HOL_LOG_SEOUL_USER_HOST:-ubuntu@100.100.1.5}"
SEOUL_SSH_KEY="${HOL_LOG_SEOUL_SSH_KEY:-${REPO_ROOT}/KR.key}"
SEOUL_HOST_KEY_ALIAS="${HOL_LOG_SEOUL_HOST_KEY_ALIAS:-100.100.1.5}"
SEOUL_REMOTE_LOG_DIR="${HOL_LOG_SEOUL_LOG_DIR:-/home/ubuntu/hololive-bot/logs}"
SEOUL_SERVICES="${HOL_LOG_SEOUL_SERVICES:-youtube-producer-b}"
SEOUL_DOCKER_SERVICES="${HOL_LOG_SEOUL_DOCKER_SERVICES:-youtube-producer youtube-producer-b}"
usage() {
  cat <<'USAGE'
Usage:
  remote-sync-main-logs.sh once osaka
  remote-sync-main-logs.sh once seoul
  remote-sync-main-logs.sh daemon <osaka|seoul> [--interval 30]
  remote-sync-main-logs.sh status <osaka|seoul>
  remote-sync-main-logs.sh query <osaka|seoul> <service> [--tail 500] [--grep pattern]
  remote-sync-main-logs.sh tail <osaka|seoul> <service>
  remote-sync-main-logs.sh docker-tail <osaka|seoul> <service> [--since 15m] [--tail 200]
Environment:
  LOG_ROOT=<repo>/logs
  HOL_LOG_OSAKA_USER_HOST=ubuntu@kapu-iris-osaka-1
  HOL_LOG_OSAKA_SSH_KEY=<repo>/KR.key
  HOL_LOG_OSAKA_HOST_KEY_ALIAS=100.100.1.7
  HOL_LOG_OSAKA_LOG_DIR=/home/ubuntu/hololive-bot/logs
  HOL_LOG_OSAKA_SERVICES=youtube-producer
  HOL_LOG_SEOUL_USER_HOST=ubuntu@100.100.1.5
  HOL_LOG_SEOUL_SSH_KEY=<repo>/KR.key
  HOL_LOG_SEOUL_HOST_KEY_ALIAS=100.100.1.5
  HOL_LOG_SEOUL_LOG_DIR=/home/ubuntu/hololive-bot/logs
  HOL_LOG_SEOUL_SERVICES=youtube-producer-b
  FORCE_MAIN_LOG_LINKS=1  # replace existing regular LOG_ROOT/<service>.log after backup
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
	    seoul) printf '%s\n' "${SEOUL_SERVICES}" ;;
	    *) echo "ERROR: unknown target: ${target}" >&2; exit 1 ;;
	esac
}
target_value() {
  local target="$1"
  local suffix="$2"
  local name
  case "${target}" in
    osaka|seoul) ;;
    *) echo "ERROR: unknown target: ${target}" >&2; exit 1 ;;
  esac
  name="${target^^}_${suffix}"
  printf '%s\n' "${!name}"
}
remote_log_service_name() {
  local target="$1"
  local service="$2"
  case "${target}:${service}" in
    osaka:youtube-producer-a) printf '%s\n' "youtube-producer" ;;
    *) printf '%s\n' "${service}" ;;
  esac
}
validate_target_service() {
  local target="$1"
  local service="$2"
  local candidate
  for candidate in $(target_services "${target}"); do
    if [[ "${candidate}" == "${service}" ]]; then
      return 0
    fi
  done
  echo "ERROR: unknown service for ${target}: ${service}" >&2
  exit 1
}
validate_docker_service() {
  local target="$1"
  local service="$2"
  local candidate
  for candidate in $(target_value "${target}" DOCKER_SERVICES); do
    if [[ "${candidate}" == "${service}" ]]; then
      return 0
    fi
  done
  echo "ERROR: unknown docker service for ${target}: ${service}" >&2
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
container_for() {
  local target="$1"
  local service="$2"
  case "${target}:${service}" in
    osaka:youtube-producer|osaka:youtube-producer-a) printf '%s\n' "hololive-youtube-producer-a" ;;
    seoul:youtube-producer|seoul:youtube-producer-b) printf '%s\n' "hololive-youtube-producer-b" ;;
    *) echo "ERROR: unknown service for ${target}: ${service}" >&2; exit 1 ;;
  esac
}
ssh_base() {
  local target="$1"
  local host_key_alias ssh_key host_key_alias_args=""
  host_key_alias="$(target_value "${target}" HOST_KEY_ALIAS)"
  ssh_key="$(target_value "${target}" SSH_KEY)"
  if [[ -n "${host_key_alias}" ]]; then
    host_key_alias_args="$(printf -- '-o HostKeyAlias=%q' "${host_key_alias}")"
  fi
  if [[ -n "${ssh_key}" ]]; then
    printf 'ssh -i %q -o IdentitiesOnly=yes -o BatchMode=yes -o ConnectTimeout=10 %s' "${ssh_key}" "${host_key_alias_args}"
  else
    printf 'ssh -o BatchMode=yes -o ConnectTimeout=10 %s' "${host_key_alias_args}"
  fi
}
rsync_ssh_base() {
  local target="$1"
  local host_key_alias ssh_key
  local host_key_alias_args=()
  host_key_alias="$(target_value "${target}" HOST_KEY_ALIAS)"
  ssh_key="$(target_value "${target}" SSH_KEY)"
  if [[ -n "${host_key_alias}" ]]; then
    host_key_alias_args=(-o "HostKeyAlias=${host_key_alias}")
  fi
  if [[ -n "${ssh_key}" ]]; then
    printf 'ssh -i %s -o IdentitiesOnly=yes -o BatchMode=yes -o ConnectTimeout=10' "${ssh_key}"
  else
    printf 'ssh -o BatchMode=yes -o ConnectTimeout=10'
  fi
  printf ' %q' "${host_key_alias_args[@]}"
}
ensure_log_root() {
  mkdir -p "${LOG_ROOT}"
  if [[ -L "${REMOTE_MIRROR_ROOT}" && ! -e "${REMOTE_MIRROR_ROOT}" ]]; then
    rm -f "${REMOTE_MIRROR_ROOT}"
  fi
  mkdir -p "${REMOTE_MIRROR_ROOT}"
}
normalize_mirror_permissions() {
  local dst="$1"
  if getent group docker >/dev/null 2>&1; then
    chgrp -R docker "${dst}" || true
  fi
  find "${dst}" -type d -exec chmod 2750 {} +
  find "${dst}" -type f -exec chmod 0640 {} +
}
ensure_service_aliases() {
  local target="$1"
  local dst service remote_service
  dst="$(target_dir "${target}")"
  for service in $(target_services "${target}"); do
    remote_service="$(remote_log_service_name "${target}" "${service}")"
    if [[ "${service}" == "${remote_service}" ]]; then
      continue
    fi
    if [[ -f "${dst}/${remote_service}.log" ]]; then
      (
        cd "${dst}"
        ln -sfn "${remote_service}.log" "${service}.log"
      )
    fi
  done
}
sync_once_remote() {
  local target="$1"
  ensure_log_root
  local dst service remote_service user_host remote_log_dir
  dst="$(target_dir "${target}")"
  mkdir -p "${dst}"
  if ! command -v rsync >/dev/null 2>&1; then
    echo "ERROR: rsync is required" >&2
    exit 1
  fi
  local rsync_includes=()
  for service in $(target_services "${target}"); do
    remote_service="$(remote_log_service_name "${target}" "${service}")"
    rsync_includes+=(--include="${remote_service}.log")
    rsync_includes+=(--include="archive/${remote_service}*")
  done
  user_host="$(target_value "${target}" USER_HOST)"
  remote_log_dir="$(target_value "${target}" REMOTE_LOG_DIR)"
  rsync -az \
    --partial \
    --delete-delay \
    --chmod=F0640,D0750 \
    --rsync-path='sudo -n rsync' \
    --include='archive/' \
    "${rsync_includes[@]}" \
    --exclude='*' \
    -e "$(rsync_ssh_base "${target}")" \
    "${user_host}:${remote_log_dir}/" \
    "${dst}/"
  ensure_service_aliases "${target}"
  date -Is > "${dst}/.last_sync"
  normalize_mirror_permissions "${dst}"
  ensure_main_links "${target}"
  echo "synced: ${user_host}:${remote_log_dir} -> ${dst}" >&2
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
status_remote() {
  local target="$1"
  local dst service link source last_sync user_host remote_log_dir
  dst="$(target_dir "${target}")"
  last_sync="never"
  [[ -f "${dst}/.last_sync" ]] && last_sync="$(cat "${dst}/.last_sync")"
  user_host="$(target_value "${target}" USER_HOST)"
  remote_log_dir="$(target_value "${target}" REMOTE_LOG_DIR)"
  echo "LOG_ROOT=${LOG_ROOT}"
  echo "remote_target=${target}"
  echo "remote_source=${user_host}:${remote_log_dir}"
  echo "mirror_dir=${dst}"
  echo "last_sync=${last_sync}"
  echo
  for service in $(target_services "${target}"); do
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
      echo "  mirror=${source} size=$(stat -Lc%s "${source}" 2>/dev/null || echo 0) mtime=$(stat -Lc%y "${source}" 2>/dev/null || echo unknown)"
    else
      echo "  mirror=${source} missing"
    fi
  done
}
daemon_remote() {
  local target="$1"
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
    sync_once_remote "${target}" || true
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
docker_tail_remote() {
  local target="$1"
  local service="$2"
  shift || true
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
  local user_host
  container="$(container_for "${target}" "${service}")"
  ssh_cmd="$(ssh_base "${target}")"
  user_host="$(target_value "${target}" USER_HOST)"
  # shellcheck disable=SC2029
  ${ssh_cmd} "${user_host}" "docker logs --timestamps --since '${since}' --tail '${tail_lines}' '${container}' 2>&1"
}
cmd="${1:-help}"
case "${cmd}" in
  once)
    target="${2:-}"
    [[ -n "${target}" ]] || { echo "ERROR: target is required" >&2; exit 1; }
    sync_once_remote "${target}"
    ;;
  daemon)
    target="${2:-}"
    [[ -n "${target}" ]] || { echo "ERROR: target is required" >&2; exit 1; }
    shift 2 || true
    daemon_remote "${target}" "$@"
    ;;
  status)
    target="${2:-}"
    [[ -n "${target}" ]] || { echo "ERROR: target is required" >&2; exit 1; }
    status_remote "${target}"
    ;;
  query)
    target="${2:-}"
    service="${3:-}"
    [[ -n "${target}" ]] || { echo "ERROR: target is required" >&2; exit 1; }
    [[ -n "${service}" ]] || { echo "ERROR: service is required" >&2; exit 1; }
    validate_target_service "${target}" "${service}"
    shift 3 || true
    query_service "${target}" "${service}" "$@"
    ;;
  tail)
    target="${2:-}"
    service="${3:-}"
    [[ -n "${target}" ]] || { echo "ERROR: target is required" >&2; exit 1; }
    [[ -n "${service}" ]] || { echo "ERROR: service is required" >&2; exit 1; }
    validate_target_service "${target}" "${service}"
    tail_service "${target}" "${service}"
    ;;
  docker-tail)
    target="${2:-}"
    service="${3:-}"
    [[ -n "${target}" ]] || { echo "ERROR: target is required" >&2; exit 1; }
    [[ -n "${service}" ]] || { echo "ERROR: service is required" >&2; exit 1; }
    validate_docker_service "${target}" "${service}"
    shift 3 || true
    docker_tail_remote "${target}" "${service}" "$@"
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
