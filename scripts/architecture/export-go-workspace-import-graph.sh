#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
REPO_CANONICAL_ROOT="$(cd "$(git -C "${ROOT_DIR}" rev-parse --path-format=absolute --git-common-dir)/.." && pwd)"
OUTPUT_FILE="${1:-${ROOT_DIR}/artifacts/architecture/go-workspace-import-graph.txt}"

resolve_shared_go_dir() {
  local candidate="${SHARED_GO_WORKSPACE_PATH:-${REPO_CANONICAL_ROOT}/../llm/shared-go}"
  if [[ ! -d "${candidate}" ]]; then
    candidate="${ROOT_DIR}/shared-go"
  fi
  if [[ ! -d "${candidate}" ]]; then
    echo "error: active shared-go dir not found" >&2
    exit 1
  fi

  printf '%s\n' "${candidate}"
}

SHARED_GO_DIR="$(resolve_shared_go_dir)"

MODULE_DIRS=(
  "${SHARED_GO_DIR}"
  "${ROOT_DIR}/hololive/hololive-shared"
  "${ROOT_DIR}/hololive/hololive-kakao-bot-go"
  "${ROOT_DIR}/hololive/hololive-dispatcher-go"
  "${ROOT_DIR}/hololive/hololive-llm-sched"
  "${ROOT_DIR}/hololive/hololive-stream-ingester"
)

mkdir -p "$(dirname "${OUTPUT_FILE}")"
tmp_edges="$(mktemp)"
cleanup() {
  rm -f "${tmp_edges}"
}
trap cleanup EXIT

for module_dir in "${MODULE_DIRS[@]}"; do
  pushd "${module_dir}" >/dev/null
  GOWORK=off go list -f '{{if not .Standard}}{{.ImportPath}}{{range .Imports}} {{.}}{{end}}{{end}}' ./...
  popd >/dev/null
done | awk '
  $1 != "" {
    from = $1
    for (i = 2; i <= NF; i++) {
      to = $i
      if (to ~ /^github.com\/(kapu\/hololive-|park285\/llm-kakao-bots\/shared-go)/) {
        printf "%s -> %s\n", from, to
      }
    }
  }
' | sort -u > "${tmp_edges}"

cp "${tmp_edges}" "${OUTPUT_FILE}"
edge_count="$(wc -l < "${OUTPUT_FILE}" | tr -d '[:space:]')"

echo "go import graph exported: ${OUTPUT_FILE}"
echo "internal edge count: ${edge_count}"
