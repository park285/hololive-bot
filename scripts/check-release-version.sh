#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

fail() {
  echo "[release-version] $*" >&2
  exit 1
}

read_semver_file() {
  local file="$1"
  local label="$2"
  local -a lines=()
  local semver_re='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'

  [[ -f "${file}" ]] || fail "${label} 파일이 없습니다: ${file}"
  mapfile -t lines <"${file}"
  [[ "${#lines[@]}" -eq 1 && -n "${lines[0]}" ]] || fail "${label}은 한 줄이어야 합니다"
  [[ "${lines[0]}" =~ ${semver_re} ]] || fail "${label}은 MAJOR.MINOR.PATCH 형식이어야 합니다: ${lines[0]}"
  printf '%s\n' "${lines[0]}"
}

version="$(read_semver_file "${ROOT_DIR}/VERSION" VERSION)"
read_semver_file "${ROOT_DIR}/hololive/hololive-api/VERSION" hololive-api >/dev/null
read_semver_file "${ROOT_DIR}/hololive/hololive-alarm-worker/VERSION" hololive-alarm-worker >/dev/null

echo "[release-version] ${version} 검증 통과"
