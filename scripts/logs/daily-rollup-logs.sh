#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${REPO_ROOT:-$(cd "${SCRIPT_DIR}/../.." && pwd)}"
LOG_ROOT="${LOG_ROOT:-${REPO_ROOT}/logs}"
ARCHIVE_DIR="${LOG_ARCHIVE_DIR:-${LOG_ROOT}/archive}"
STATE_DIR="${LOG_ROLLUP_STATE_DIR:-${REPO_ROOT}/.tmp/daily-rollup}"
RETENTION_DAYS="${LOG_ARCHIVE_RETENTION_DAYS:-${LOG_MAX_AGE_DAYS:-30}}"
ROLLUP_DATE="${LOG_ROLLUP_DATE:-$(date -d 'yesterday' +%F)}"
LOCK_DIR="${LOG_ROLLUP_LOCK_DIR:-${STATE_DIR}/lock}"
ROLLUP_FILES="${LOG_ROLLUP_FILES:-*.log}"

usage() {
  cat <<'USAGE'
Usage:
  daily-rollup-logs.sh once

Environment:
  LOG_ROOT=<repo>/logs
  LOG_ARCHIVE_DIR=<LOG_ROOT>/archive
  LOG_ROLLUP_STATE_DIR=<repo>/.tmp/daily-rollup
  LOG_ROLLUP_DATE=YYYY-MM-DD
  LOG_ROLLUP_FILES='*.log'
  LOG_ARCHIVE_RETENTION_DAYS=30

Behavior:
  Archives each matching top-level regular log file into
  LOG_ARCHIVE_DIR/<service>-YYYY-MM-DD.log.tar.gz by copying a snapshot and
  truncating the same inode, so long-lived file descriptors keep writing to the
  visible log path after rollup. Symlinked logs are skipped so remote mirrors
  are not modified centrally. Files with more than one hard link are refused
  and left unchanged because copytruncate cannot preserve their aliases safely.
USAGE
}

die() {
  echo "ERROR: $*" >&2
  exit 1
}

validate_rollup_date() {
  [[ "${ROLLUP_DATE}" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] || die "LOG_ROLLUP_DATE must be YYYY-MM-DD"
}

ensure_private_dir() {
  local dir="$1"

  [[ -L "${dir}" ]] && die "private state path is a symlink: ${dir}"
  mkdir -p "${dir}"
  chmod 0700 "${dir}" 2>/dev/null || true
  [[ -d "${dir}" && ! -L "${dir}" ]] || die "private state path is not a directory: ${dir}"
}

acquire_rollup_lock() {
  ensure_private_dir "$(dirname "${LOCK_DIR}")"
  if ! mkdir "${LOCK_DIR}" 2>/dev/null; then
    die "daily rollup is already running or lock path is unsafe: ${LOCK_DIR}"
  fi

  trap release_rollup_lock EXIT
}

release_rollup_lock() {
  rmdir "${LOCK_DIR}" 2>/dev/null || true
}

snapshot_and_truncate_log() {
  local log_file="$1" tmp_file="$2"

  python3 - "${log_file}" "${tmp_file}" <<'PY'
import errno
import os
import stat
import sys

log_file, tmp_file = sys.argv[1:3]
skip = 75
empty = 76
unsafe_links = 77

flags = os.O_RDWR | os.O_APPEND
if hasattr(os, "O_NOFOLLOW"):
    flags |= os.O_NOFOLLOW

try:
    log_fd = os.open(log_file, flags)
except OSError as exc:
    if exc.errno in (errno.ENOENT, errno.ELOOP):
        sys.exit(skip)
    raise

try:
    st = os.fstat(log_fd)
    if not stat.S_ISREG(st.st_mode):
        sys.exit(skip)
    if st.st_nlink != 1:
        sys.exit(unsafe_links)
    if st.st_size <= 0:
        sys.exit(empty)

    tmp_fd = os.open(tmp_file, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        try:
            os.fchown(tmp_fd, st.st_uid, st.st_gid)
        except PermissionError:
            pass
        os.fchmod(tmp_fd, stat.S_IMODE(st.st_mode))

        remaining = st.st_size
        os.lseek(log_fd, 0, os.SEEK_SET)
        while remaining > 0:
            chunk = os.read(log_fd, min(1024 * 1024, remaining))
            if not chunk:
                break
            os.write(tmp_fd, chunk)
            remaining -= len(chunk)
    finally:
        os.close(tmp_fd)

    tail_chunks = []
    while True:
        chunk = os.read(log_fd, 1024 * 1024)
        if not chunk:
            break
        tail_chunks.append(chunk)

    current = os.fstat(log_fd)
    if not stat.S_ISREG(current.st_mode) or current.st_nlink != 1:
        sys.exit(unsafe_links)

    os.ftruncate(log_fd, 0)
    for chunk in tail_chunks:
        os.write(log_fd, chunk)
finally:
    os.close(log_fd)
PY
}

archive_exists() {
  local archive_path="$1"

  [[ -e "${archive_path}" || -L "${archive_path}" ]]
}

publish_archive() {
  local source="$1" dest="$2" dest_dir dest_base

  dest_dir="$(dirname "${dest}")"
  dest_base="$(basename "${dest}")"

  if ! python3 - "${source}" "${dest_dir}" "${dest_base}" <<'PY'
import os
import sys

source, dest_dir, dest_base = sys.argv[1:4]
flags = os.O_RDONLY | os.O_DIRECTORY
if hasattr(os, "O_NOFOLLOW"):
    flags |= os.O_NOFOLLOW

dir_fd = os.open(dest_dir, flags)
try:
    os.link(source, dest_base, dst_dir_fd=dir_fd, follow_symlinks=False)
finally:
    os.close(dir_fd)
PY
  then
    return 1
  fi

  rm -f -- "${source}"
}

archive_one_log() {
  local log_file="$1"
  local base service archive_path tmp_dir tmp_file archive_tmp rc

  [[ -f "${log_file}" && ! -L "${log_file}" ]] || return 0
  [[ -s "${log_file}" ]] || return 0

  base="$(basename "${log_file}")"
  service="${base%.log}"
  archive_path="${ARCHIVE_DIR}/${service}-${ROLLUP_DATE}.log.tar.gz"
  if archive_exists "${archive_path}"; then
    echo "skip existing archive: ${archive_path}"
    return 0
  fi

  tmp_dir="$(mktemp -d "${STATE_DIR}/stage-${service}.XXXXXX")"
  tmp_file="${tmp_dir}/${base}"
  archive_tmp="${tmp_dir}/${service}-${ROLLUP_DATE}.log.tar.gz"

  if snapshot_and_truncate_log "${log_file}" "${tmp_file}"; then
    :
  else
    rc=$?
    if [[ "${rc}" -eq 75 ]]; then
      echo "skip raced non-regular log: ${log_file}" >&2
      rm -rf "${tmp_dir}"
      return 0
    fi
    if [[ "${rc}" -eq 76 ]]; then
      rm -rf "${tmp_dir}"
      return 0
    fi
    if [[ "${rc}" -eq 77 ]]; then
      rm -rf "${tmp_dir}"
      die "refusing copytruncate for multi-link log: ${log_file}"
    fi
    rm -rf "${tmp_dir}"
    die "snapshot/truncate failed for ${log_file}"
  fi

  if [[ ! -f "${tmp_file}" || -L "${tmp_file}" ]]; then
    echo "skip raced non-regular log: ${log_file}" >&2
    rm -rf "${tmp_dir}"
    return 0
  fi

  tar -C "${tmp_dir}" -czf "${archive_tmp}" -- "${base}"
  chown --no-dereference --reference="${tmp_file}" "${archive_tmp}" || true
  chmod 0640 "${archive_tmp}" || true
  publish_archive "${archive_tmp}" "${archive_path}" || die "archive publish failed; preserved staging at ${tmp_dir}"
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
  ensure_private_dir "${STATE_DIR}"
  mkdir -p "${LOG_ROOT}" "${ARCHIVE_DIR}"
  [[ -d "${ARCHIVE_DIR}" && ! -L "${ARCHIVE_DIR}" ]] || die "archive path is not a directory: ${ARCHIVE_DIR}"

  acquire_rollup_lock

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

  release_rollup_lock
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
