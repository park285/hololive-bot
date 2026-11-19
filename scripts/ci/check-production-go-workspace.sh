#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

dockerfiles=(
  "hololive/hololive-api/Dockerfile"
  "hololive/hololive-alarm-worker/Dockerfile"
  "hololive/hololive-youtube-collector/Dockerfile"
  "hololive/hololive-youtube-collector/Dockerfile.po-sandbox"
)

module_dirs=(
  "hololive/hololive-api"
  "hololive/hololive-alarm-worker"
  "hololive/hololive-youtube-collector"
  "hololive/hololive-shared"
  "hololive/hololive-dbtest"
)

for file in "${dockerfiles[@]}"; do
  path="${root}/${file}"
  [[ -f "${path}" ]] || { echo "[production-workspace] missing ${file}" >&2; exit 1; }

  grep -Eq '^ENV[[:space:]]+GOWORK=off([[:space:]]|$)' "${path}" || {
    echo "[production-workspace] ${file} must disable workspace module resolution" >&2
    exit 1
  }
  grep -q 'GOPROXY=https://proxy.golang.org' "${path}" || {
    echo "[production-workspace] ${file} must use the checksummed public Go module proxy without direct VCS fallback" >&2
    exit 1
  }
  if grep -Eq '(^|[[:space:]])apk[[:space:]]+add([[:space:]]|$)' "${path}"; then
    echo "[production-workspace] ${file} installs mutable Alpine packages during the build" >&2
    exit 1
  fi
done

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

echo "[production-workspace] production builds use the checksummed module proxy, no build-time apk installs, and stable published external pins"
