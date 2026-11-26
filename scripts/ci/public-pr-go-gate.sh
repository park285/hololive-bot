#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

usage() {
  echo "usage: $0 <module> <tidy|vet|race|race-json|test-prod|build-prod>" >&2
  exit 2
}

(( $# == 2 )) || usage
module="$1"
stage="$2"

case "${module}" in
  hololive/hololive-api|hololive/hololive-alarm-worker|hololive/hololive-dbtest|hololive/hololive-shared|hololive/hololive-collector)
    ;;
  *)
    echo "unsupported public PR module: ${module}" >&2
    exit 2
    ;;
esac

case "${stage}" in
  tidy|vet|race|race-json|test-prod|build-prod)
    ;;
  *)
    echo "unsupported public PR stage: ${stage}" >&2
    exit 2
    ;;
esac

if [[ "${stage}" == @(race-json|test-prod|build-prod) ]]; then
  if [[ "${module}" != "hololive/hololive-collector" ]]; then
    echo "race-json, test-prod and build-prod are only valid for hololive/hololive-collector" >&2
    exit 2
  fi
  . "${SCRIPT_DIR}/python-runtime.sh"
  repo_python_init
fi

module_dir="${ROOT_DIR}/${module}"

[[ -f "${module_dir}/go.mod" ]] || {
  echo "go.mod is missing for public PR module: ${module}" >&2
  exit 1
}

export GOWORK=off
export GOMAXPROCS="${GOMAXPROCS:-2}"
export GOMEMLIMIT="${GOMEMLIMIT:-5GiB}"

cd "${module_dir}"

use_shared_test_database() {
  [[ -z "${TEST_DATABASE_URL:-}" ]] || return 0
  source "${SCRIPT_DIR}/local-ci-integration.sh"
  provision_integration_test_database
}

run_with_failure_tail() {
  local label="$1"
  shift
  local output_file
  output_file="$(mktemp)"
  if ! "$@" >"${output_file}" 2>&1; then
    echo "::error title=${label} failed::module=${module} stage=${stage}"
    tail -n 400 "${output_file}" >&2
    rm -f "${output_file}"
    return 1
  fi
  rm -f "${output_file}"
}

run_collector_json_tests() {
  local jsonl test_status=0 json_status=0
  jsonl="$(mktemp)"
  go test -json "$@" ./... >"${jsonl}" || test_status=$?
  if [[ "${stage}" == race-json && "${test_status}" == 0 ]]; then
    bash "${SCRIPT_DIR}/go-test-nonrace.sh" --cgo-disabled --json ./... >>"${jsonl}" || test_status=$?
  fi
  "${CI_PYTHON_BIN}" "${ROOT_DIR}/scripts/ci/check-go-test-json.py" \
    --input "${jsonl}" \
    --require-pass \
    --allow-skip-file "${ROOT_DIR}/scripts/ci/collector-test-skip-allowlist.txt" || json_status=$?
  if (( test_status != 0 || json_status != 0 )); then
    echo "::error title=collector JSON tests failed::module=${module} stage=${stage}"
    tail -n 400 "${jsonl}" >&2
    rm -f "${jsonl}"
    return 1
  fi
  rm -f "${jsonl}"
}

case "${stage}" in
  tidy)
    echo "[public-pr] module=${module} go mod tidy -diff"
    go mod tidy -diff
    ;;
  vet)
    export GOFLAGS="${GOFLAGS:+${GOFLAGS} }-mod=readonly"
    echo "[public-pr] module=${module} go vet ./..."
    go vet ./...
    ;;
  race|race-json)
    export GOFLAGS="${GOFLAGS:+${GOFLAGS} }-mod=readonly"
    use_shared_test_database
    echo "[public-pr] module=${module} go test -race -p 2 -count=1 ./..."
    if [[ "${stage}" == race-json ]]; then
      run_collector_json_tests -race -p 2 -count=1
    else
      run_with_failure_tail "race tests" go test -race -p 2 -count=1 ./...
      run_with_failure_tail "non-race-only tests" bash "${SCRIPT_DIR}/go-test-nonrace.sh" ./...
    fi
    ;;
  test-prod)
    export GOFLAGS="${GOFLAGS:+${GOFLAGS} }-mod=readonly"
    use_shared_test_database
    echo "[public-pr] module=${module} CGO_ENABLED=0 go test -json -count=1 ./..."
    CGO_ENABLED=0 run_collector_json_tests -count=1
    ;;
  build-prod)
    export GOFLAGS="${GOFLAGS:+${GOFLAGS} }-mod=readonly"
    if [[ -n "${RUNNER_TEMP:-}" ]]; then
      [[ "${RUNNER_TEMP}" == /* ]] || {
        echo "RUNNER_TEMP must be an absolute path" >&2
        exit 1
      }
      work_root="${RUNNER_TEMP}"
      mkdir -p "${work_root}"
    else
      work_root="$(mktemp -d)"
      trap 'rm -rf "${work_root}"' EXIT
    fi
    out_dir="${work_root}/collector-build-prod"
    mkdir -p "${out_dir}"
    revision="$(git -C "${ROOT_DIR}" rev-parse --verify 'HEAD^{commit}')"
    version="${HOLO_BOT_VERSION:-$(git -C "${ROOT_DIR}" rev-parse --short=12 HEAD)}"
    echo "[public-pr] module=${module} build-hololive-collector-go.sh version=${version} revision=${revision}"
    sh "${ROOT_DIR}/scripts/build/build-hololive-collector-go.sh" \
      --output-dir "${out_dir}" \
      --version "${version}" \
      --revision "${revision}" \
      --goos linux \
      --goarch amd64 \
      --goamd64 v1
    sh "${ROOT_DIR}/scripts/build/check-hololive-collector-go-artifact.sh" "${out_dir}" \
      --version "${version}" \
      --revision "${revision}" \
      --goos linux \
      --goarch amd64 \
      --goamd64 v1
    ;;
esac
