#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

pass() {
  echo "[PASS] $*"
}

if ! docker version >/dev/null 2>&1; then
  echo "[SKIP] docker daemon unavailable" >&2
  exit 0
fi

context_filelist() {
  local ctx="$1"
  local img
  img="$(docker build -q -f - "${ctx}" 2>/dev/null <<'DOCKERFILE'
FROM busybox
COPY . /ctx
RUN find /ctx -type f | sort > /filelist.txt
DOCKERFILE
)" || return 1
  docker run --rm "${img}" cat /filelist.txt
  docker rmi "${img}" >/dev/null 2>&1 || true
}

assert_excluded() {
  local listing="$1" path="$2" label="$3"
  if grep -qF -- "${path}" <<<"${listing}"; then
    fail "hb03: ${label} leaked into build context: ${path}"
  fi
}

assert_present() {
  local listing="$1" path="$2" label="$3"
  if ! grep -qF -- "${path}" <<<"${listing}"; then
    fail "hb03: required ${label} missing from build context: ${path}"
  fi
}

build_fixture() {
  local ctx="$1" dockerignore="$2"
  mkdir -p "${ctx}/hololive/hololive-api/config" "${ctx}/hololive/hololive-api/coverage" "${ctx}/hololive/hololive-api/artifacts" \
    "${ctx}/hololive/hololive-api/internal/logs"
  cp "${dockerignore}" "${ctx}/.dockerignore"
  printf 'secret\n' > "${ctx}/hololive/hololive-api/config/credentials.json"
  printf 'secret\n' > "${ctx}/hololive/hololive-api/config/service-account.json"
  printf 'secret\n' > "${ctx}/hololive/hololive-api/config/serviceaccount.json"
  printf 'secret\n' > "${ctx}/hololive/hololive-api/config/tls.key"
  printf 'secret\n' > "${ctx}/hololive/hololive-api/config/.env.production"
  printf 'log\n' > "${ctx}/hololive/hololive-api/config/debug.log"
  printf 'coverage\n' > "${ctx}/hololive/hololive-api/coverage/coverage.out"
  printf 'artifact\n' > "${ctx}/hololive/hololive-api/artifacts/stale.bin"
  printf 'package config\n' > "${ctx}/hololive/hololive-api/config/loader.go"
  printf 'package logs\n' > "${ctx}/hololive/hololive-api/internal/logs/logger.go"
  printf 'secret\n' > "${ctx}/hololive/hololive-api/internal/logs/.env.production"
  printf 'secret\n' > "${ctx}/hololive/hololive-api/internal/logs/tls.key"
  printf 'certificate\n' > "${ctx}/hololive/hololive-api/internal/logs/client.pem"
  printf 'certificate\n' > "${ctx}/hololive/hololive-api/internal/logs/client.crt"
  printf 'secret\n' > "${ctx}/hololive/hololive-api/internal/logs/credentials.json"
  printf 'secret\n' > "${ctx}/hololive/hololive-api/internal/logs/service-account.json"
  printf 'secret\n' > "${ctx}/hololive/hololive-api/internal/logs/serviceaccount.json"
  printf 'secret\n' > "${ctx}/hololive/hololive-api/internal/logs/mysecret.yaml"
  mkdir -p "${ctx}/hololive/hololive-api/internal"
  printf 'package api\n' > "${ctx}/hololive/hololive-api/internal/source.go"
}

assert_api_sensitive_excluded() {
  local listing="$1" label="$2"
  for secret in credentials.json service-account.json serviceaccount.json tls.key .env.production debug.log; do
    assert_excluded "${listing}" "/ctx/hololive/hololive-api/config/${secret}" "${label}/${secret}"
  done
  assert_excluded "${listing}" "/ctx/hololive/hololive-api/coverage/coverage.out" "${label}/coverage"
  assert_excluded "${listing}" "/ctx/hololive/hololive-api/artifacts/stale.bin" "${label}/artifacts"

  for secret in .env.production tls.key client.pem client.crt credentials.json service-account.json serviceaccount.json mysecret.yaml; do
    assert_excluded "${listing}" "/ctx/hololive/hololive-api/internal/logs/${secret}" "${label}/internal-logs/${secret}"
  done
}

producer_ctx="${TMP_DIR}/producer"
build_fixture "${producer_ctx}" "${ROOT_DIR}/hololive/hololive-youtube-collector/Dockerfile.dockerignore"
producer_list="$(context_filelist "${producer_ctx}")" || fail "hb03: producer fixture build failed"

assert_api_sensitive_excluded "${producer_list}" "producer"
assert_excluded "${producer_list}" "/ctx/hololive/hololive-api/config/loader.go" "producer admin source"

api_ctx="${TMP_DIR}/api"
build_fixture "${api_ctx}" "${ROOT_DIR}/hololive/hololive-api/Dockerfile.dockerignore"
api_list="$(context_filelist "${api_ctx}")" || fail "hb03: hololive-api fixture build failed"

assert_api_sensitive_excluded "${api_list}" "hololive-api"
assert_present "${api_list}" "/ctx/hololive/hololive-api/config/loader.go" "hololive-api module source"
assert_present "${api_list}" "/ctx/hololive/hololive-api/internal/source.go" "hololive-api module source"

root_ctx="${TMP_DIR}/root"
build_fixture "${root_ctx}" "${ROOT_DIR}/.dockerignore"
root_list="$(context_filelist "${root_ctx}")" || fail "hb03: root fixture build failed"

assert_api_sensitive_excluded "${root_list}" "root"
assert_present "${root_list}" "/ctx/hololive/hololive-api/config/loader.go" "root-context source"

pass "hb03: API secrets excluded from producer + api + root build context, sources retained where required (dd36cc1a, 3559884b)"

alarm_ctx="${TMP_DIR}/alarm"
build_fixture "${alarm_ctx}" "${ROOT_DIR}/hololive/hololive-alarm-worker/Dockerfile.dockerignore"
alarm_list="$(context_filelist "${alarm_ctx}")" || fail "hb03: alarm-worker fixture build failed"

assert_api_sensitive_excluded "${alarm_list}" "alarm-worker"
assert_excluded "${alarm_list}" "/ctx/hololive/hololive-api/config/loader.go" "alarm-worker admin source (module-standalone build)"

pass "Go runtime contexts exclude credentials and retain only required module sources"
