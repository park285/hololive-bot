#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

forbidden_files=(
  "${ROOT_DIR}/hololive/hololive-kakao-bot-go/internal/server/shared_compat.go"
  "${ROOT_DIR}/hololive/hololive-kakao-bot-go/internal/server/api_trigger_compat.go"
  "${ROOT_DIR}/hololive/hololive-shared/internal/envutil/env.go"
  "${ROOT_DIR}/hololive/hololive-shared/pkg/logging/logging.go"
)

found_file=0
for file in "${forbidden_files[@]}"; do
  if [[ -f "${file}" ]]; then
    if [[ "${found_file}" -eq 0 ]]; then
      echo "FAIL: forbidden compatibility adapter files detected:" >&2
      found_file=1
    fi
    echo "  - ${file}" >&2
  fi
done

if [[ "${found_file}" -ne 0 ]]; then
  exit 1
fi

tmp_alias_hits="$(mktemp)"
tmp_import_hits="$(mktemp)"
cleanup() {
  rm -f "${tmp_alias_hits}" "${tmp_import_hits}"
}
trap cleanup EXIT

rg -n 'type\s+[A-Za-z0-9_]+\s*=\s*sharedserver\.' \
  "${ROOT_DIR}/hololive/hololive-kakao-bot-go/internal/server" \
  -g '*.go' > "${tmp_alias_hits}" || true

if [[ -s "${tmp_alias_hits}" ]]; then
  echo "FAIL: sharedserver compatibility type alias is forbidden" >&2
  cat "${tmp_alias_hits}" >&2
  exit 1
fi

rg -n 'github.com/kapu/hololive-shared/internal/envutil' \
  "${ROOT_DIR}/hololive" -g '*.go' > "${tmp_import_hits}" || true

if [[ -s "${tmp_import_hits}" ]]; then
  echo "FAIL: forbidden internal envutil import detected" >&2
  cat "${tmp_import_hits}" >&2
  exit 1
fi

rg -n 'github.com/kapu/hololive-shared/pkg/logging' \
  "${ROOT_DIR}/hololive" -g '*.go' > "${tmp_import_hits}" || true

if [[ -s "${tmp_import_hits}" ]]; then
  echo "FAIL: forbidden shared logging wrapper import detected" >&2
  cat "${tmp_import_hits}" >&2
  exit 1
fi

echo "OK: no forbidden Go compatibility adapters"
