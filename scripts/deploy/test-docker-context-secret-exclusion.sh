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
  mkdir -p "${ctx}/admin-dashboard/backend/config" "${ctx}/admin-dashboard/backend/coverage" "${ctx}/admin-dashboard/backend/artifacts"
  cp "${dockerignore}" "${ctx}/.dockerignore"
  printf 'secret\n' > "${ctx}/admin-dashboard/backend/config/credentials.json"
  printf 'secret\n' > "${ctx}/admin-dashboard/backend/config/service-account.json"
  printf 'secret\n' > "${ctx}/admin-dashboard/backend/config/serviceaccount.json"
  printf 'secret\n' > "${ctx}/admin-dashboard/backend/config/tls.key"
  printf 'secret\n' > "${ctx}/admin-dashboard/backend/config/.env.production"
  printf 'log\n' > "${ctx}/admin-dashboard/backend/config/debug.log"
  printf 'coverage\n' > "${ctx}/admin-dashboard/backend/coverage/coverage.out"
  printf 'artifact\n' > "${ctx}/admin-dashboard/backend/artifacts/stale.bin"
  printf 'package config\n' > "${ctx}/admin-dashboard/backend/config/loader.go"
  mkdir -p "${ctx}/hololive/hololive-api/internal"
  printf 'package api\n' > "${ctx}/hololive/hololive-api/internal/source.go"
}

assert_admin_backend_sensitive_excluded() {
  local listing="$1" label="$2"
  for secret in credentials.json service-account.json serviceaccount.json tls.key .env.production debug.log; do
    assert_excluded "${listing}" "/ctx/admin-dashboard/backend/config/${secret}" "${label}/${secret}"
  done
  assert_excluded "${listing}" "/ctx/admin-dashboard/backend/coverage/coverage.out" "${label}/coverage"
  assert_excluded "${listing}" "/ctx/admin-dashboard/backend/artifacts/stale.bin" "${label}/artifacts"
}

producer_ctx="${TMP_DIR}/producer"
build_fixture "${producer_ctx}" "${ROOT_DIR}/hololive/hololive-youtube-producer/Dockerfile.dockerignore"
producer_list="$(context_filelist "${producer_ctx}")" || fail "hb03: producer fixture build failed"

assert_admin_backend_sensitive_excluded "${producer_list}" "producer"
assert_excluded "${producer_list}" "/ctx/admin-dashboard/backend/config/loader.go" "producer admin source"

api_ctx="${TMP_DIR}/api"
build_fixture "${api_ctx}" "${ROOT_DIR}/hololive/hololive-api/Dockerfile.dockerignore"
api_list="$(context_filelist "${api_ctx}")" || fail "hb03: hololive-api fixture build failed"

assert_admin_backend_sensitive_excluded "${api_list}" "hololive-api"
assert_excluded "${api_list}" "/ctx/admin-dashboard/backend/config/loader.go" "hololive-api admin source (module-standalone build)"
assert_present "${api_list}" "/ctx/hololive/hololive-api/internal/source.go" "hololive-api module source"

root_ctx="${TMP_DIR}/root"
build_fixture "${root_ctx}" "${ROOT_DIR}/.dockerignore"
root_list="$(context_filelist "${root_ctx}")" || fail "hb03: root fixture build failed"

assert_admin_backend_sensitive_excluded "${root_list}" "root"
assert_present "${root_list}" "/ctx/admin-dashboard/backend/config/loader.go" "root-context source"

pass "hb03: admin backend secrets excluded from producer + api + root build context, sources retained where required (dd36cc1a, 3559884b)"

alarm_ctx="${TMP_DIR}/alarm"
build_fixture "${alarm_ctx}" "${ROOT_DIR}/hololive/hololive-alarm-worker/Dockerfile.dockerignore"
alarm_list="$(context_filelist "${alarm_ctx}")" || fail "hb03: alarm-worker fixture build failed"

assert_admin_backend_sensitive_excluded "${alarm_list}" "alarm-worker"
assert_excluded "${alarm_list}" "/ctx/admin-dashboard/backend/config/loader.go" "alarm-worker admin source (module-standalone build)"

admin_ctx="${TMP_DIR}/admin"
build_fixture "${admin_ctx}" "${ROOT_DIR}/admin-dashboard/Dockerfile.dockerignore"
admin_list="$(context_filelist "${admin_ctx}")" || fail "hb03: admin-dashboard fixture build failed"

assert_admin_backend_sensitive_excluded "${admin_list}" "admin-dashboard"
assert_present "${admin_list}" "/ctx/admin-dashboard/backend/config/loader.go" "admin-dashboard backend source (own context)"
assert_excluded "${admin_list}" "/ctx/hololive/hololive-api/internal/source.go" "admin-dashboard non-dependency module (hololive-api)"

pass "hb03: alarm-worker + admin-dashboard Dockerfile.dockerignore exclude admin backend secrets; admin retains its own backend source, drops non-dependency modules"

named_ctx="${TMP_DIR}/named"
shared_ctx="${TMP_DIR}/shared"
mkdir -p "${named_ctx}" "${shared_ctx}/pkg" "${shared_ctx}/artifacts"
printf 'module x\n' > "${shared_ctx}/go.mod"
printf '\n' > "${shared_ctx}/go.sum"
printf 'package p\n' > "${shared_ctx}/pkg/p.go"
printf 'stale-untracked\n' > "${shared_ctx}/artifacts/stale.bin"
printf 'local-uncommitted\n' > "${shared_ctx}/local-uncommitted.go"

producer_named="$(grep -E '^COPY --from=shared_go_workspace' "${ROOT_DIR}/hololive/hololive-youtube-producer/Dockerfile")"
{
  printf 'FROM busybox\n'
  while IFS= read -r line; do
    printf '%s\n' "${line/\/workspace\/shared-go//w/shared-go}"
  done <<<"${producer_named}"
  printf 'RUN find /w -type f | sort > /fl.txt\n'
} > "${named_ctx}/Dockerfile"

named_img="$(docker build -q --build-context shared_go_workspace="${shared_ctx}" "${named_ctx}" 2>/dev/null)" \
  || fail "hb03: named-context fixture build failed"
named_list="$(docker run --rm "${named_img}" cat /fl.txt)"
docker rmi "${named_img}" >/dev/null 2>&1 || true

assert_excluded "${named_list}" "/w/shared-go/artifacts/stale.bin" "named-context stale artifact (9eedece2)"
assert_excluded "${named_list}" "/w/shared-go/local-uncommitted.go" "named-context uncommitted file (9eedece2)"
assert_present "${named_list}" "/w/shared-go/go.mod" "named-context go.mod"
assert_present "${named_list}" "/w/shared-go/pkg/p.go" "named-context pkg source"

pass "hb03: shared-go named-context COPY pulls only module-verified paths (9eedece2)"
