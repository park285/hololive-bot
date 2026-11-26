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

QUEUED_TESTS=()

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
  QUEUED_TESTS+=("${test}")
}

run_queued_tests() {
  local logs test index status
  local -a pids=() failed=()
  (( ${#QUEUED_TESTS[@]} )) || return 0
  logs="$(mktemp -d)"
  echo "[pre-push] running ${#QUEUED_TESTS[@]} script tests in parallel"
  for test in "${QUEUED_TESTS[@]}"; do
    case "${test}" in
      *.py) "${CI_PYTHON_BIN}" "${test}" ;;
      *) bash "${test}" ;;
    esac >"${logs}/${#pids[@]}.log" 2>&1 &
    pids+=("$!")
  done
  for index in "${!QUEUED_TESTS[@]}"; do
    status=0
    wait "${pids[index]}" || status=$?
    echo "[pre-push] test: ${QUEUED_TESTS[index]}"
    cat "${logs}/${index}.log"
    if (( status != 0 )); then
      echo "[pre-push] test failed (exit ${status}): ${QUEUED_TESTS[index]}" >&2
      failed+=("${QUEUED_TESTS[index]}")
    fi
  done
  rm -rf -- "${logs}"
  QUEUED_TESTS=()
  if (( ${#failed[@]} )); then
    printf '[pre-push] failed script tests: %s\n' "${failed[*]}" >&2
    return 1
  fi
}

run_content_gates() {
  echo "[pre-push] stack lint rules and comment ban (ast-grep, SQL, TLA+, Lean)"
  bash scripts/ci/verify-tools.sh scan
  echo "[pre-push] formal verification (TLC, Lean)"
  bash scripts/ci/verify-tools.sh formal

  if inputs_changed deploy/quadlet; then
    QUADLET_UNIT_DIRS="${ROOT_DIR}/deploy/quadlet/hololive-bot" /usr/libexec/podman/quadlet -dryrun >/dev/null
  fi
  run_if_changed scripts/ci/check-recurring-security-scan-contract.sh \
    scripts/ci/final-image-scan-manifest.txt deploy/quadlet
  echo "[pre-push] deploy/runtime script tests (run when the test or its target changed)"
  run_if_changed scripts/deploy/lib/worker-profile-pair_test.py scripts/deploy/lib/worker-profile-pair.py scripts/deploy/lib/collector-slots.tsv \
    deploy/quadlet/hololive-bot
  run_if_changed scripts/build/po-sandbox-manifest_test.py \
    scripts/build/po-sandbox-manifest.py scripts/deploy/lib/po-sandbox-image.sh
  run_queued_tests

  local run_collector_js=false
  if inputs_changed hololive/hololive-collector scripts/ci/public-pr-collector-helper-install.sh; then
    run_collector_js=true
  fi
  echo "[pre-push] mode=${PRE_PUSH_MODE} local_ci_go_scope=${LOCAL_CI_GO_SCOPE}"
  source "${SCRIPT_DIR}/local-ci.sh"
  run_local_ci "${run_collector_js}"
}

run_dependency_hygiene() {
  local module govulncheck_bin

  govulncheck_bin="$(ensure_govulncheck)"
  for module in "${GO_PRODUCT_MODULES[@]}"; do
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
run_queued_tests
if is_metadata_only_route; then
  if inputs_changed VERSION hololive/hololive-api/VERSION hololive/hololive-alarm-worker/VERSION; then
    source "${SCRIPT_DIR}/go-workspace-modules.sh"
    for module in "${GO_PRODUCT_MODULES[@]}"; do
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
