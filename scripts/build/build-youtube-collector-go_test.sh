#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "${ROOT_DIR}/scripts/ci/python-runtime.sh"
repo_python_init
BUILD="${ROOT_DIR}/scripts/build/build-youtube-collector-go.sh"
GATE="${ROOT_DIR}/scripts/ci/public-pr-go-gate.sh"

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "${TMP_ROOT}"' EXIT

expect_exit() {
  local expected="$1" label="$2" status=0 output
  shift 2
  output="$("$@" 2>&1)" || status=$?
  if (( status != expected )); then
    printf 'FAIL: %s (expected %d, got %d)\n%s\n' "${label}" "${expected}" "${status}" "${output}" >&2
    return 1
  fi
  printf 'ok - %s\n' "${label}"
}

check_artifact() {
  local dir="$1"
  sh "${ROOT_DIR}/scripts/build/check-youtube-collector-go-artifact.sh" "${dir}" \
    --version "${2:-${version}}" \
    --revision "${revision}" \
    --goos linux \
    --goarch amd64 \
    --goamd64 v1
}

build_input() {
  sh "${BUILD}" --output-dir "${TMP_ROOT}/candidate" --version "$1" --revision "$2" \
    --goos linux --goarch amd64 --goamd64 v1
}

revision="$(git -C "${ROOT_DIR}" rev-parse --verify 'HEAD^{commit}')"
version="${HOLO_BOT_VERSION:-$(git -C "${ROOT_DIR}" rev-parse --short=12 HEAD)}"

expect_exit 2 'invalid revision' build_input test not-a-revision
expect_exit 2 'unknown revision without opt-in' build_input test unknown
expect_exit 2 'unsafe version' build_input 'bad version' "${revision}"

HOLO_BOT_VERSION="${version}" RUNNER_TEMP="${RUNNER_TEMP:-${TMP_ROOT}}" bash "${GATE}" hololive/hololive-youtube-collector build-prod
out_dir="${RUNNER_TEMP:-${TMP_ROOT}}/collector-build-prod"
expect_exit 1 'valid artifact from a different release' check_artifact "${out_dir}" "${version}-mismatch"
fixture="${TMP_ROOT}/altered"
mkdir -p "${fixture}/bin"
cp -L "${out_dir}/bin/"{youtube-collector,healthcheck} "${fixture}/bin/"
cp -L "${out_dir}/sha256sums.txt" "${fixture}/"
jq '.source_revision = ("0" * 40)' "${out_dir}/manifest.json" >"${fixture}/manifest.json"
expect_exit 1 'manifest identity mismatch' check_artifact "${fixture}"
jq --arg wrong "${version}-mismatch" '.version = $wrong' "${out_dir}/manifest.json" >"${fixture}/manifest.json"
expect_exit 1 'incorrect manifest version' check_artifact "${fixture}"

jq '.files["bin/youtube-collector"] = ("0" * 64)' "${out_dir}/manifest.json" >"${fixture}/manifest.json"
expect_exit 1 'incorrect binary hash in manifest' check_artifact "${fixture}"
cp "${out_dir}/manifest.json" "${fixture}/manifest.json"

printf '%064d  %s\n' 0 bin/youtube-collector >"${fixture}/sha256sums.txt"
expect_exit 1 'incorrect checksum file' check_artifact "${fixture}"

cp "${fixture}/bin/youtube-collector" "${fixture}/bin/healthcheck"
jq '.files["bin/healthcheck"] = .files["bin/youtube-collector"]' "${out_dir}/manifest.json" >"${fixture}/manifest.json"
(cd "${fixture}" && sha256sum bin/youtube-collector bin/healthcheck >sha256sums.txt)
status=0
diagnostic="$(check_artifact "${fixture}" 2>&1)" || status=$?
if [[ "${status}" != 1 || "${diagnostic}" != *'bin/healthcheck package'* ]]; then
  printf 'FAIL: substituted healthcheck not identified: %s\n' "${diagnostic}" >&2
  exit 1
fi
echo 'ok - collector substituted for healthcheck with matching checksums'
