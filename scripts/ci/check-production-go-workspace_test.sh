#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
checker="${root}/scripts/ci/check-production-go-workspace.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

fixture_files=(
  admin-dashboard/Dockerfile
  admin-dashboard/backend/go.mod
  hololive/hololive-api/Dockerfile
  hololive/hololive-api/go.mod
  hololive/hololive-alarm-worker/Dockerfile
  hololive/hololive-alarm-worker/go.mod
  hololive/hololive-youtube-producer/Dockerfile
  hololive/hololive-youtube-producer/go.mod
  hololive/hololive-shared/go.mod
  hololive/hololive-dbtest/go.mod
  deploy/compose/docker-compose.prod.yml
  deploy/compose/docker-compose.osaka.yml
  deploy/compose/docker-compose.osaka2.yml
  deploy/compose/docker-compose.seoul.yml
)

build_fixture() {
  local destination="$1" file
  mkdir -p "${destination}"
  for file in "${fixture_files[@]}"; do
    mkdir -p "${destination}/$(dirname "${file}")"
    cp "${root}/${file}" "${destination}/${file}"
  done
}

assert_rejects() {
	local name="$1" expected="$2"
	local destination="${tmp}/${name}"
	build_fixture "${destination}"
	case "${name}" in
		workspace-enabled)
			sed -i 's#^ENV GOWORK=off.*#ENV GOWORK=/workspace/go.work#' "${destination}/hololive/hololive-api/Dockerfile"
			;;
		mutable-context)
			printf '\n# additional_contexts:\n#   shared_go_workspace: ../shared-go\n' >>"${destination}/deploy/compose/docker-compose.prod.yml"
			;;
		unpublished-pin)
			sed -i 's#github.com/park285/shared-go v1.37.1#github.com/park285/shared-go v1.37.2-0.20260731000000-deadbeefdead#' "${destination}/hololive/hololive-api/go.mod"
			;;
		*)
			echo "[production-workspace-test] unknown mutation ${name}" >&2
			exit 1
			;;
	esac
  if PRODUCTION_GO_WORKSPACE_ROOT="${destination}" bash "${checker}" >"${destination}/out" 2>&1; then
    echo "[production-workspace-test] ${name} mutation survived" >&2
    exit 1
  fi
  grep -Fq "${expected}" "${destination}/out" || {
    cat "${destination}/out" >&2
    exit 1
  }
}

baseline="${tmp}/baseline"
build_fixture "${baseline}"
PRODUCTION_GO_WORKSPACE_ROOT="${baseline}" bash "${checker}" >/dev/null

assert_rejects workspace-enabled \
	'must build from hololive/hololive-api/go.mod with GOWORK=off'

assert_rejects mutable-context \
	'production Compose must not inject mutable sibling build contexts'

assert_rejects unpublished-pin \
	'must pin github.com/park285/shared-go to a stable published tag'

echo "ok: production Go dependency provenance gate rejects mutable and unpublished inputs"
