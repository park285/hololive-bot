#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

usage() {
  echo "usage: $0 <module> <tidy|vet|test|race|test-prod|build-prod>" >&2
  exit 2
}

(( $# == 2 )) || usage
module="$1"
stage="$2"

case "${module}" in
  .|admin-dashboard/backend|hololive/hololive-api|hololive/hololive-alarm-worker|hololive/hololive-dbtest|hololive/hololive-shared|hololive/hololive-youtube-collector)
    ;;
  *)
    echo "unsupported public PR module: ${module}" >&2
    exit 2
    ;;
esac

case "${stage}" in
  tidy|vet|test|race|test-prod|build-prod)
    ;;
  *)
    echo "unsupported public PR stage: ${stage}" >&2
    exit 2
    ;;
esac

if [[ "${stage}" == "test-prod" || "${stage}" == "build-prod" ]]; then
  if [[ "${module}" != "hololive/hololive-youtube-collector" ]]; then
    echo "test-prod and build-prod are only valid for hololive/hololive-youtube-collector" >&2
    exit 2
  fi
fi

module_dir="${ROOT_DIR}"
if [[ "${module}" != "." ]]; then
  module_dir="${ROOT_DIR}/${module}"
fi

[[ -f "${module_dir}/go.mod" ]] || {
  echo "go.mod is missing for public PR module: ${module}" >&2
  exit 1
}

export GOWORK=off
export GOMAXPROCS="${GOMAXPROCS:-2}"
export GOMEMLIMIT="${GOMEMLIMIT:-5GiB}"

cd "${module_dir}"

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

prepare_test_environment() {
  if [[ "${module}" == "." ]]; then
    # The root package contains a local stack orchestration test that recursively
    # invokes sibling shared-go/iris-client-go checkouts. PUBLIC PR CI validates
    # every in-repo module directly; the cross-repository suite remains owned by
    # the canonical local pre-push workspace gate.
    export HOLOLIVE_WORKSPACE_MONOREPO_TEST=1
  fi
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
  test)
    export GOFLAGS="${GOFLAGS:+${GOFLAGS} }-mod=readonly"
    prepare_test_environment
    echo "[public-pr] module=${module} go test -count=1 ./..."
    run_with_failure_tail "unit tests" go test -count=1 ./...
    ;;
  race)
    export GOFLAGS="${GOFLAGS:+${GOFLAGS} }-mod=readonly"
    prepare_test_environment
    echo "[public-pr] module=${module} go test -race -p 2 -count=1 ./..."
    run_with_failure_tail "race tests" go test -race -p 2 -count=1 ./...
    ;;
  test-prod)
    export GOFLAGS="${GOFLAGS:+${GOFLAGS} }-mod=readonly"
    prepare_test_environment
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
    jsonl="${work_root}/collector-go-sonic.jsonl"
    echo "[public-pr] module=${module} CGO_ENABLED=0 go test -json -count=1 -tags sonic ./..."
    set +e
    set +o pipefail
    CGO_ENABLED=0 go test -json -count=1 -tags sonic ./... | tee "${jsonl}"
    pipeline_status=("${PIPESTATUS[@]}")
    test_status="${pipeline_status[0]}"
    tee_status="${pipeline_status[1]}"
    set -euo pipefail
    if [[ "${tee_status}" -ne 0 ]]; then
      echo "failed to record sonic test JSON" >&2
      exit 1
    fi
    python3 "${ROOT_DIR}/scripts/ci/check-go-test-json.py" \
      --input "${jsonl}" \
      --require-pass \
      --allow-skip-file "${ROOT_DIR}/scripts/ci/collector-test-skip-allowlist.txt"
    if [[ "${test_status}" -ne 0 ]]; then
      echo "::error title=sonic tests failed::module=${module} stage=${stage}"
      exit 1
    fi
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
    echo "[public-pr] module=${module} build-youtube-collector-go.sh version=${version} revision=${revision}"
    sh "${ROOT_DIR}/scripts/build/build-youtube-collector-go.sh" \
      --output-dir "${out_dir}" \
      --version "${version}" \
      --revision "${revision}" \
      --goos linux \
      --goarch amd64 \
      --goamd64 v1
    sh "${ROOT_DIR}/scripts/build/check-youtube-collector-go-artifact.sh" "${out_dir}" \
      --version "${version}" \
      --revision "${revision}" \
      --goos linux \
      --goarch amd64 \
      --goamd64 v1
    ;;
esac
