#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${REPO_ROOT:-$(cd "${SCRIPT_DIR}/../.." && pwd)}"
LOG_ROOT="${LOG_ROOT:-${REPO_ROOT}/logs}"
ARCHIVE_DIR="${LOG_ARCHIVE_DIR:-${LOG_ROOT}/archive}"
RETENTION_DAYS="${LOG_ARCHIVE_RETENTION_DAYS:-${LOG_MAX_AGE_DAYS:-30}}"
ROLLUP_DATE="${LOG_ROLLUP_DATE:-$(date -d 'yesterday' +%F)}"
LOCK_FILE="${LOG_ROLLUP_LOCK_FILE:-${LOG_ROOT}/.daily-rollup.lock}"
ROLLUP_FILES="${LOG_ROLLUP_FILES:-*.log}"

usage() {
  cat <<'USAGE'
Usage:
  daily-rollup-logs.sh once

Environment:
  LOG_ROOT=<repo>/logs
  LOG_ARCHIVE_DIR=<LOG_ROOT>/archive
  LOG_ROLLUP_DATE=YYYY-MM-DD
  LOG_ROLLUP_FILES='*.log'
  LOG_ARCHIVE_RETENTION_DAYS=30

Behavior:
  Archives each matching top-level regular log file into
  LOG_ARCHIVE_DIR/<service>-YYYY-MM-DD.log.tar.gz, then truncates the active
  file. Symlinked logs are skipped so remote mirrors are not modified centrally.
USAGE
}

die() {
  echo "ERROR: $*" >&2
  exit 1
}

validate_rollup_date() {
  [[ "${ROLLUP_DATE}" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] || die "LOG_ROLLUP_DATE must be YYYY-MM-DD"
}

archive_one_log() {
  local log_file="$1"
  local base service archive_path tmp_dir tmp_file

  [[ -f "${log_file}" && ! -L "${log_file}" ]] || return 0
  [[ -s "${log_file}" ]] || return 0

  base="$(basename "${log_file}")"
  service="${base%.log}"
  archive_path="${ARCHIVE_DIR}/${service}-${ROLLUP_DATE}.log.tar.gz"
  if [[ -e "${archive_path}" ]]; then
    echo "skip existing archive: ${archive_path}"
    return 0
  fi

  tmp_dir="$(mktemp -d "${LOG_ROOT}/.daily-rollup-${service}.XXXXXX")"
  tmp_file="${tmp_dir}/${base}"
  mv "${log_file}" "${tmp_file}"
  ( set -C; : > "${log_file}" ) 2>/dev/null || true
  chown --reference="${tmp_file}" "${log_file}" 2>/dev/null || true
  chmod --reference="${tmp_file}" "${log_file}" 2>/dev/null || true
  tar -C "${tmp_dir}" -czf "${archive_path}" "${base}"
  chown --reference="${tmp_file}" "${archive_path}" || true
  chmod 0640 "${archive_path}" || true
  rm -rf "${tmp_dir}"
  echo "archived: ${log_file} -> ${archive_path}"
}

prune_old_archives() {
  [[ "${RETENTION_DAYS}" =~ ^[0-9]+$ ]] || die "LOG_ARCHIVE_RETENTION_DAYS must be an integer"
  if [[ "${RETENTION_DAYS}" -gt 0 ]]; then
    find "${ARCHIVE_DIR}" -maxdepth 1 -type f -name '*.log.tar.gz' -mtime +"${RETENTION_DAYS}" -delete
  fi
}

rollup_once() {
  validate_rollup_date
  mkdir -p "${LOG_ROOT}" "${ARCHIVE_DIR}"

  exec 9>"${LOCK_FILE}"
  flock -n 9 || die "daily rollup is already running"

  local pattern log_file found=0
  for pattern in ${ROLLUP_FILES}; do
    for log_file in "${LOG_ROOT}"/${pattern}; do
      [[ -e "${log_file}" ]] || continue
      found=1
      archive_one_log "${log_file}"
    done
  done
  prune_old_archives

  if [[ "${found}" -eq 0 ]]; then
    echo "no top-level log files found under ${LOG_ROOT}"
  fi
}

cmd="${1:-once}"
case "${cmd}" in
  once)
    rollup_once
    ;;
  help|-h|--help)
    usage
    ;;
  *)
    die "unknown command: ${cmd}"
    ;;
esac
