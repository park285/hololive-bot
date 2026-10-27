#!/usr/bin/env bash
set -euo pipefail

root="${PRODUCTION_GO_WORKSPACE_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"

dockerfiles=(
  "hololive/hololive-api/Dockerfile"
  "hololive/hololive-alarm-worker/Dockerfile"
  "hololive/hololive-youtube-collector/Dockerfile"
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
  if grep -Eq '^replace[[:space:]]+github\.com/park285/(shared-go|iris-client-go)' "${gomod}"; then
    echo "[production-workspace] ${module}/go.mod replaces a published external module with mutable source" >&2
    exit 1
  fi

  for dependency in github.com/park285/shared-go/v2 github.com/park285/iris-client-go/v4; do
    version="$(awk -v dependency="${dependency}" '$1 == dependency { print $2; exit }' "${gomod}")"
    [[ -z "${version}" ]] && continue
    if [[ ! "${version}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      echo "[production-workspace] ${module}/go.mod must pin ${dependency} to a stable published tag, got ${version}" >&2
      exit 1
    fi
  done
done

echo "[production-workspace] production builds use the checksummed module proxy, no build-time apk installs, and stable published external pins"
