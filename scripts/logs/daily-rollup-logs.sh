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
  LOG_ARCHIVE_DIR/<service>-YYYY-MM-DD.log.tar.gz by atomically moving it aside
  and recreating a fresh empty file in its place (mv + recreate, not copy +
  truncate), so no write is lost to a copy/truncate race. This assumes the log
  files are rsync mirrors / non-long-lived-fd writers; a process holding the
  file open across the rollup would keep writing to the moved inode. Symlinked
  logs are skipped so remote mirrors are not modified centrally.
USAGE
}

die() {
  echo "ERROR: $*" >&2
  exit 1
}

validate_rollup_date() {
  [[ "${ROLLUP_DATE}" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] || die "LOG_ROLLUP_DATE must be YYYY-MM-DD"
}

# mv 로 비워진 경로에 새 빈 로그를 재생성한다. 공격자가 LOG_ROOT 쓰기 권한으로 그 경로에
# symlink 를 심으면 path 기반 chown/chmod 가 host 파일을 follow 해 root 권한상승이 된다.
# 그래서 우리 소유 0700 tmp_dir 안에서 owner/mode 를 맞춘 정규 파일을 만든 뒤 mv -fT 로
# rename 한다. rename(2) 은 목적지 symlink 를 follow 하지 않고 그 이름을 원자 교체하므로,
# 권한 설정은 attacker 가 닿지 못하는 tmp 에서만 일어나고 교체는 symlink 를 안전히 덮어쓴다.
recreate_empty_log() {
  local log_file="$1" ref_file="$2" tmp_dir="$3"
  local fresh="${tmp_dir}/.recreate.tmp"

  : > "${fresh}"
  chown --reference="${ref_file}" "${fresh}" 2>/dev/null || true
  chmod --reference="${ref_file}" "${fresh}" 2>/dev/null || true
  mv -fT "${fresh}" "${log_file}"
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
  recreate_empty_log "${log_file}" "${tmp_file}" "${tmp_dir}"
  tar -C "${tmp_dir}" -czf "${archive_path}" "${base}"
  chown --no-dereference --reference="${tmp_file}" "${archive_path}" || true
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

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
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
fi
