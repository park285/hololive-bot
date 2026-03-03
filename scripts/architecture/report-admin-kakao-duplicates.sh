#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
ADMIN_DIR="${ROOT_DIR}/hololive/hololive-admin"
KAKAO_DIR="${ROOT_DIR}/hololive/hololive-kakao-bot-go"

SCOPES=(
  "internal/server"
  "internal/app"
  "internal/service/auth"
)

tmp_admin="$(mktemp)"
tmp_kakao="$(mktemp)"
tmp_dup="$(mktemp)"
cleanup() {
  rm -f "${tmp_admin}" "${tmp_kakao}" "${tmp_dup}"
}
trap cleanup EXIT

echo "# admin↔kakao 중복 파일 리포트"
echo

total=0
for scope in "${SCOPES[@]}"; do
  admin_scope="${ADMIN_DIR}/${scope}"
  kakao_scope="${KAKAO_DIR}/${scope}"

  if [[ ! -d "${admin_scope}" || ! -d "${kakao_scope}" ]]; then
    continue
  fi

  find "${admin_scope}" -type f -name '*.go' -printf '%P\n' | sort > "${tmp_admin}"
  find "${kakao_scope}" -type f -name '*.go' -printf '%P\n' | sort > "${tmp_kakao}"
  comm -12 "${tmp_admin}" "${tmp_kakao}" > "${tmp_dup}"

  scope_count="$(wc -l < "${tmp_dup}" | tr -d '[:space:]')"
  total=$((total + scope_count))

  echo "## ${scope} (중복 ${scope_count}개)"
  if [[ "${scope_count}" -eq 0 ]]; then
    echo "- 없음"
    echo
    continue
  fi

  sed 's/^/- /' "${tmp_dup}"
  echo
done

echo "총 중복 파일 수: ${total}"
