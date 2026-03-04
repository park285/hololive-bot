#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
OUTPUT_FILE="${1:-${ROOT_DIR}/artifacts/architecture/go-workspace-import-graph.txt}"

MODULE_DIRS=(
  "shared-go"
  "hololive/hololive-shared"
  "hololive/hololive-kakao-bot-go"
  "hololive/hololive-dispatcher-go"
  "hololive/hololive-llm-sched"
  "hololive/hololive-stream-ingester"
)

mkdir -p "$(dirname "${OUTPUT_FILE}")"
tmp_edges="$(mktemp)"
cleanup() {
  rm -f "${tmp_edges}"
}
trap cleanup EXIT

for module_dir in "${MODULE_DIRS[@]}"; do
  pushd "${ROOT_DIR}/${module_dir}" >/dev/null
  go list -f '{{if not .Standard}}{{.ImportPath}}{{range .Imports}} {{.}}{{end}}{{end}}' ./...
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
