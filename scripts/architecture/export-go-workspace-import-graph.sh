#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
OUTPUT_FILE="${1:-${ROOT_DIR}/artifacts/architecture/go-workspace-import-graph.txt}"

source "${SCRIPT_DIR}/lib/git_guard.sh"
require_git_checkout "${ROOT_DIR}"

resolve_shared_go_dir() {
  local candidate="${SHARED_GO_WORKSPACE_PATH:-${ROOT_DIR}/shared-go}"
  if [[ ! -d "${candidate}" ]]; then
    echo "error: active shared-go dir not found: ${candidate}" >&2
    exit 1
  fi

  (cd "${candidate}" && pwd)
}

SHARED_GO_DIR="$(resolve_shared_go_dir)"

MODULE_DIRS=(
  "${ROOT_DIR}"
  "${SHARED_GO_DIR}"
  "${ROOT_DIR}/hololive/hololive-shared"
  "${ROOT_DIR}/hololive/hololive-kakao-bot-go"
  "${ROOT_DIR}/hololive/hololive-admin-api"
  "${ROOT_DIR}/hololive/hololive-alarm-worker"
  "${ROOT_DIR}/hololive/hololive-dispatcher-go"
  "${ROOT_DIR}/hololive/hololive-llm-sched"
  "${ROOT_DIR}/hololive/hololive-stream-ingester"
)

for module_dir in "${MODULE_DIRS[@]}"; do
  if [[ ! -f "${module_dir}/go.mod" ]]; then
    echo "error: expected Go module is missing go.mod: ${module_dir}" >&2
    exit 1
  fi
done

mkdir -p "$(dirname "${OUTPUT_FILE}")"
tmp_edges="$(mktemp)"
tmp_modules="$(mktemp)"
tmp_work_dir="$(mktemp -d)"
cleanup() {
  rm -f "${tmp_edges}" "${tmp_modules}"
  rm -rf "${tmp_work_dir}"
}
trap cleanup EXIT

for module_dir in "${MODULE_DIRS[@]}"; do
  (cd "${module_dir}" && GOWORK=off go list -m -f '{{.Path}}')
done | LC_ALL=C sort -u > "${tmp_modules}"

(
  cd "${tmp_work_dir}"
  go work init "${MODULE_DIRS[@]}"
)
tmp_go_work="${tmp_work_dir}/go.work"

for module_dir in "${MODULE_DIRS[@]}"; do
  pushd "${module_dir}" >/dev/null
  GOWORK="${tmp_go_work}" go list -f '{{if not .Standard}}{{.ImportPath}}{{range .Imports}} {{.}}{{end}}{{range .TestImports}} {{.}}{{end}}{{range .XTestImports}} {{.}}{{end}}{{end}}' ./...
  popd >/dev/null
done | awk -v modules_file="${tmp_modules}" '
  BEGIN {
    while ((getline line < modules_file) > 0) {
      if (line != "") {
        allowed[line] = 1
      }
    }
    close(modules_file)
  }
  function is_internal(path, module) {
    return path == module || index(path, module "/") == 1
  }
  $1 != "" {
    from = $1
    for (i = 2; i <= NF; i++) {
      to = $i
      for (module in allowed) {
        if (is_internal(to, module)) {
          printf "%s -> %s\n", from, to
          break
        }
      }
    }
  }
' | LC_ALL=C sort -u > "${tmp_edges}"

cp "${tmp_edges}" "${OUTPUT_FILE}"
edge_count="$(wc -l < "${OUTPUT_FILE}" | tr -d '[:space:]')"

echo "go import graph exported: ${OUTPUT_FILE}"
echo "internal edge count: ${edge_count}"
