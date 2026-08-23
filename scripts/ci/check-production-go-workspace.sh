#!/usr/bin/env bash
set -euo pipefail

root="${PRODUCTION_GO_WORKSPACE_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"

docker_modules=(
  "hololive/hololive-api/Dockerfile|hololive/hololive-api"
  "hololive/hololive-alarm-worker/Dockerfile|hololive/hololive-alarm-worker"
  "hololive/hololive-youtube-collector/Dockerfile|hololive/hololive-youtube-collector"
  "admin-dashboard/Dockerfile|admin-dashboard/backend"
)

module_dirs=(
  "admin-dashboard/backend"
  "hololive/hololive-api"
  "hololive/hololive-alarm-worker"
  "hololive/hololive-youtube-collector"
  "hololive/hololive-shared"
  "hololive/hololive-dbtest"
)

for record in "${docker_modules[@]}"; do
  IFS='|' read -r file module <<<"${record}"
  path="${root}/${file}"
  [[ -f "${path}" ]] || { echo "[production-workspace] missing ${file}" >&2; exit 1; }

  grep -Eq '^ENV GOWORK=off([[:space:]]|$)' "${path}" || {
    echo "[production-workspace] ${file} must build from ${module}/go.mod with GOWORK=off" >&2
    exit 1
  }
  grep -q 'GOPROXY=https://proxy.golang.org' "${path}" || {
    echo "[production-workspace] ${file} must use the checksummed public Go module proxy without direct VCS fallback" >&2
    exit 1
  }
  if grep -Eq 'go work (init|use)|go mod edit[[:space:]]+-replace|--from=(shared_go_workspace|iris_client_go_workspace)' "${path}"; then
    echo "[production-workspace] ${file} uses a mutable sibling source or mutates module resolution" >&2
    exit 1
  fi
  if grep -Eq '(^|[[:space:]])apk[[:space:]]+add([[:space:]]|$)' "${path}"; then
    echo "[production-workspace] ${file} installs mutable Alpine packages during the build" >&2
    exit 1
  fi
  grep -Fq "COPY ${module} ./${module}" "${path}" || {
    echo "[production-workspace] ${file} does not copy its authoritative module source" >&2
    exit 1
  }
done

if rg -n 'shared_go_workspace|iris_client_go_workspace|additional_contexts:' "${root}/deploy/compose" --glob 'docker-compose*.yml' >/dev/null; then
  echo "[production-workspace] production Compose must not inject mutable sibling build contexts" >&2
  exit 1
fi

for module in "${module_dirs[@]}"; do
  gomod="${root}/${module}/go.mod"
  [[ -f "${gomod}" ]] || { echo "[production-workspace] missing ${module}/go.mod" >&2; exit 1; }
  if grep -Eq '^replace[[:space:]]+github\.com/park285/(shared-go|iris-client-go)' "${gomod}"; then
    echo "[production-workspace] ${module}/go.mod replaces a published external module with mutable source" >&2
    exit 1
  fi

  for dependency in github.com/park285/shared-go/v2 github.com/park285/iris-client-go/v2; do
    version="$(awk -v dependency="${dependency}" '$1 == dependency { print $2; exit }' "${gomod}")"
    [[ -z "${version}" ]] && continue
    if [[ ! "${version}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      echo "[production-workspace] ${module}/go.mod must pin ${dependency} to a stable published tag, got ${version}" >&2
      exit 1
    fi
  done
done

echo "[production-workspace] production builds use GOWORK=off, in-repo module sources, and stable published external pins"
