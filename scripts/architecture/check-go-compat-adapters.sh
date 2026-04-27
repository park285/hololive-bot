#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

if ! command -v rg >/dev/null 2>&1; then
  echo "FAIL: ripgrep (rg) is required for compatibility-adapter checks" >&2
  exit 1
fi

admin_server_dir="${ROOT_DIR}/hololive/hololive-admin-api/internal/server"
bot_server_dir="${ROOT_DIR}/hololive/hololive-kakao-bot-go/internal/server"

if [[ ! -d "${admin_server_dir}" ]]; then
  echo "FAIL: expected admin-api server directory is missing: ${admin_server_dir}" >&2
  exit 1
fi

forbidden_files=(
  "${admin_server_dir}/shared_compat.go"
  "${admin_server_dir}/api_trigger_compat.go"
  "${admin_server_dir}/settings_types.go"
  "${admin_server_dir}/settings_result.go"
  "${admin_server_dir}/api_response.go"
  "${bot_server_dir}/shared_compat.go"
  "${bot_server_dir}/api_trigger_compat.go"
  "${bot_server_dir}/settings_types.go"
  "${bot_server_dir}/settings_result.go"
  "${bot_server_dir}/api_response.go"
  "${ROOT_DIR}/hololive/hololive-shared/internal/envutil/env.go"
  "${ROOT_DIR}/hololive/hololive-shared/pkg/logging/logging.go"
  "${ROOT_DIR}/hololive/hololive-llm-sched/internal/app/delivery_providers_local.go"
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

server_search_dirs=("${admin_server_dir}")
if [[ -d "${bot_server_dir}" ]]; then
  server_search_dirs+=("${bot_server_dir}")
fi

rg -n 'type\s+[A-Za-z0-9_]+\s*=\s*[A-Za-z0-9_]*sharedserver\.' \
  "${server_search_dirs[@]}" \
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
