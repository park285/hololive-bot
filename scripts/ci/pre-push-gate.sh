#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
if (( $# > 0 )); then
  echo "usage: $0" >&2
  exit 2
fi

if [[ -z "${PRE_PUSH_GATE_SCOPED:-}" ]] \
  && command -v systemd-run >/dev/null 2>&1 \
  && systemd-run --user --scope --quiet -p MemoryHigh=1G true >/dev/null 2>&1; then
  echo "[pre-push] memory scope: MemoryHigh=${PRE_PUSH_MEMORY_HIGH:-24G} MemoryMax=${PRE_PUSH_MEMORY_MAX:-32G}"
  export PRE_PUSH_GATE_SCOPED=1
  exec systemd-run --user --scope --quiet \
    -p RuntimeMaxSec=2h \
    -p KillMode=control-group \
    -p "MemoryHigh=${PRE_PUSH_MEMORY_HIGH:-24G}" \
    -p "MemoryMax=${PRE_PUSH_MEMORY_MAX:-32G}" \
    "${SCRIPT_DIR}/pre-push-gate.sh"
fi

cd "${ROOT_DIR}"

export GOTOOLCHAIN="${GOTOOLCHAIN-auto}"

unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_PREFIX
source "${SCRIPT_DIR}/local-ci-packages.sh"

resolve_route() {
  local route_range
  route_exact=false
  changed_files=()
  if [[ -n "${BASE_SHA:-}" && -n "${HEAD_SHA:-}" ]]; then
    route_range="${BASE_SHA}..${HEAD_SHA}"
    require_checkout_head "${HEAD_SHA}" || return 1
    if ! load_changed_files "${route_range}"; then
      echo "failed to resolve exact pushed range ${route_range}" >&2
      return 1
    fi
    route_exact=true
  fi

  if [[ "${FULL_PRE_PUSH:-false}" == "true" ]]; then
    PRE_PUSH_MODE=full
  else
    PRE_PUSH_MODE="${PRE_PUSH_MODE:-fast}"
  fi

  case "${PRE_PUSH_MODE}" in
    fast|full) ;;
    *)
      echo "unsupported PRE_PUSH_MODE=${PRE_PUSH_MODE}; expected fast or full" >&2
      return 1
      ;;
  esac

  if [[ "${PRE_PUSH_MODE}" == full || "${route_exact}" != true ]]; then
    LOCAL_CI_GO_SCOPE=all
  else
    LOCAL_CI_GO_SCOPE="${LOCAL_CI_GO_SCOPE:-changed}"
  fi
}

is_metadata_only_route() {
  local file
  [[ "${route_exact}" == true && "${PRE_PUSH_MODE}" != full ]] || return 1
  for file in "${changed_files[@]}"; do
    is_metadata_path "${file}" || return 1
  done
  return 0
}

inputs_changed() {
  local input file
  [[ "${PRE_PUSH_MODE}" != full && "${route_exact}" == true ]] || return 0
  for input in "$@"; do
    for file in "${changed_files[@]}"; do
      case "${file}" in
        "${input}"|"${input}"/*) return 0 ;;
      esac
    done
  done
  return 1
}

run_if_changed() {
  local test="$1" input inputs_valid=true
  shift
  for input in "${test}" "$@"; do
    if [[ ! -e "${input}" ]]; then
      echo "[pre-push] test input missing: ${input} (declared for ${test})" >&2
      inputs_valid=false
    fi
  done
  if [[ "${inputs_valid}" == true ]] && ! inputs_changed "${test}" "$@"; then
    echo "[pre-push] test skipped (inputs unchanged): ${test}"
    return 0
  fi
  echo "[pre-push] test: ${test}"
  case "${test}" in
    *.py) "${CI_PYTHON_BIN}" "${test}" ;;
    *) bash "${test}" ;;
  esac
}

run_content_gates() {
  echo "[pre-push] stack lint rules and comment ban (ast-grep, SQL, TLA+, Lean)"
  bash scripts/ci/verify-tools.sh scan
  echo "[pre-push] formal verification (TLC, Lean)"
  bash scripts/ci/verify-tools.sh formal

  run_if_changed scripts/deploy/admin-bind_test.sh scripts/deploy/lib/admin-bind.sh scripts/deploy/quadlet-prestart.sh \
    scripts/deploy/lib/public-bind-mounts.sh scripts/deploy/lib/compose-env.sh \
    scripts/systemd/admin-dashboard-ingress.nft deploy/topology/hosts.conf
  if inputs_changed deploy/quadlet; then
    QUADLET_UNIT_DIRS="${ROOT_DIR}/deploy/quadlet/hololive-bot" /usr/libexec/podman/quadlet -dryrun >/dev/null
  fi
  run_if_changed scripts/deploy/sync-opt-current_test.sh \
    scripts/deploy/sync-opt-current.sh scripts/deploy/quadlet-prestart.sh scripts/deploy/lib \
    scripts/ci/postgres-capacity-policy.tsv scripts/systemd deploy/postgres deploy/nginx deploy/quadlet
  run_if_changed scripts/deploy/quadlet-redeploy-service_test.sh \
    scripts/deploy/quadlet-redeploy-service.sh scripts/deploy/quadlet-rollback-service.sh \
    scripts/deploy/lib/postgres-capacity.sh scripts/ci/postgres-capacity-policy.tsv \
    scripts/deploy/lib/worker-profile-pair.py deploy/quadlet
  run_if_changed scripts/ci/check-recurring-security-scan-contract.sh \
    scripts/ci/final-image-scan-policy.sh scripts/ci/final-image-scan-manifest.txt deploy/quadlet
  echo "[pre-push] deploy/runtime script tests (run when the test or its target changed)"
  run_if_changed scripts/build/build-all_test.sh \
    build-all.sh scripts/deploy/lib/source-revision.sh
  run_if_changed scripts/build/image-runtime-tree-permissions_test.sh \
    hololive/hololive-youtube-collector/{Dockerfile,Dockerfile.po-sandbox}{,.dockerignore} \
    hololive/hololive-alarm-worker/Dockerfile{,.dockerignore} \
    hololive/hololive-youtube-collector/{po-sandbox,youtubejs}/{package.json,package-lock.json,src} \
    hololive/hololive-alarm-worker/xspaces/{package.json,package-lock.json,src}
  run_if_changed scripts/deploy/lib/youtubejs-node-version_test.sh scripts/deploy/lib/youtubejs-node-version.sh
  run_if_changed scripts/deploy/source-revision-provenance_test.sh scripts/deploy/lib/source-revision.sh
  run_if_changed scripts/deploy/lib/worker-profile-pair_test.py scripts/deploy/lib/worker-profile-pair.py deploy/quadlet/hololive-bot
  run_if_changed scripts/deploy/kapu-d-cutover_test.py scripts/deploy/kapu-d-cutover.py deploy/podman/kapu/hololive-youtube-collector-d.container deploy/podman/kapu/hololive-youtube-po-d.container
  run_if_changed scripts/build/po-sandbox-manifest_test.py \
    scripts/build/po-sandbox-manifest.py scripts/deploy/lib/po-sandbox-image.sh
  run_if_changed scripts/logs/daily-rollup-logs_test.sh scripts/logs/daily-rollup-logs.sh
  run_if_changed scripts/logs/test-stream-mirror-retention.sh scripts/logs/lib/stream.sh
  run_if_changed scripts/deploy/verify-exec-tree-ownership_test.sh scripts/deploy/verify-exec-tree-ownership.sh
  run_if_changed scripts/deploy/lib/public-bind-mounts_test.sh scripts/deploy/lib/public-bind-mounts.sh deploy/nginx
  run_if_changed scripts/runtime/set-iris-base-url_test.sh scripts/runtime/set-iris-base-url.sh
  run_if_changed scripts/deploy/ap-host-native-deploy_test.sh \
    scripts/deploy/ap-host-native-deploy.sh scripts/deploy/ap-host-native-rollback.sh \
    scripts/deploy/ap-host-native-collector-wrapper_test.sh scripts/deploy/ap-host-native-cutover_test.sh \
    scripts/deploy/collector-cutover-failure_test.sh \
    scripts/deploy/ap-completion-check.sh \
    scripts/deploy/lib scripts/logs/ap-host-native-status.sh
  run_if_changed scripts/deploy/ap-completion-check_test.sh \
    scripts/deploy/ap-completion-check.sh scripts/deploy/ap-hosts scripts/deploy/lib
  run_if_changed scripts/deploy/lib/ap-host_test.sh scripts/deploy/lib/ap-host.sh scripts/deploy/ap-hosts
  run_if_changed hololive/hololive-api/scripts/test-bot-env-loader.sh \
    hololive/hololive-api/scripts/bot.sh scripts/deploy/lib/compose-env.sh

  local run_collector_js=false test_collector_artifacts=false
  if inputs_changed hololive/hololive-youtube-collector scripts/ci/public-pr-collector-helper-install.sh; then
    run_collector_js=true
  fi
  if inputs_changed scripts/build/build-youtube-collector-go{,_test}.sh scripts/build/check-youtube-collector-go-artifact.sh \
    scripts/ci/public-pr-go-gate.sh scripts/ci/python-{runtime,runner}.sh .python-version; then
    test_collector_artifacts=true
  fi
  echo "[pre-push] mode=${PRE_PUSH_MODE} local_ci_go_scope=${LOCAL_CI_GO_SCOPE}"
  source "${SCRIPT_DIR}/local-ci.sh"
  run_local_ci "${run_collector_js}" "${test_collector_artifacts}"
}

run_dependency_hygiene() {
  local module govulncheck_bin

  govulncheck_bin="$(ensure_govulncheck)"
  for module in "${GO_OWNED_MODULES[@]}"; do
    echo "[pre-push] dependency hygiene: ${module}"
    (cd "${module}" && GOWORK=off "${govulncheck_bin}" ./...)
  done
}

echo "════════════════════════════════════════"
echo "  pre-push quality gate"
echo "════════════════════════════════════════"
resolve_route
run_if_changed scripts/check-release-version.sh VERSION hololive/hololive-api/VERSION \
  hololive/hololive-alarm-worker/VERSION
if is_metadata_only_route; then
  if inputs_changed VERSION hololive/hololive-api/VERSION hololive/hololive-alarm-worker/VERSION; then
    source "${SCRIPT_DIR}/go-workspace-modules.sh"
    for module in "${GO_OWNED_MODULES[@]}"; do
      echo "[pre-push] version-only build: ${module}"
      (cd "${module}" && GOWORK=off go build -mod=readonly ./...)
    done
  fi
  echo "[pre-push] documentation/version-only gate passed"
else
  . "${SCRIPT_DIR}/python-runtime.sh"
  repo_python_init
  run_content_gates
  run_dependency_hygiene
fi
echo "════════════════════════════════════════"
echo "  pre-push quality gate passed"
echo "════════════════════════════════════════"