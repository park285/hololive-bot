#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

module_dirs=(
  "hololive/hololive-api"
  "hololive/hololive-alarm-worker"
  "hololive/hololive-collector"
  "hololive/hololive-shared"
  "hololive/hololive-dbtest"
)

for module in "${module_dirs[@]}"; do
  gomod="${root}/${module}/go.mod"
  [[ -f "${gomod}" ]] || { echo "[production-workspace] missing ${module}/go.mod" >&2; exit 1; }
  violations="$(GOWORK=off go mod edit -json "${gomod}" | jq -r '
    def external: test("^github\\.com/park285/(shared-go|iris-client-go)(/|$)");
    (.Replace[]? | select(.Old.Path | external) | "must not replace \(.Old.Path)"),
    (.Require[]? | select(.Path | external) | select(.Version | test("^v[0-9]+\\.[0-9]+\\.[0-9]+$") | not)
      | "\(.Path) must have a stable published tag, got \(.Version)")
  ')"
  if [[ -n "${violations}" ]]; then
    printf '[production-workspace] %s/go.mod: %s\n' "${module}" "${violations}" >&2
    exit 1
  fi
done

echo "[production-workspace] production modules use stable published external pins without local replacements"
