#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "${ROOT_DIR}/scripts/deploy/lib/compose-services.sh"

fail() {
    echo "[FAIL] $*" >&2
    exit 1
}

pass() {
    echo "[PASS] $*"
}

expect_eq() {
    local actual="$1"
    local expected="$2"
    local label="$3"

    if [[ "${actual}" != "${expected}" ]]; then
        fail "${label}: expected '${expected}', got '${actual}'"
    fi
    pass "${label}"
}

expect_fail() {
    local label="$1"
    shift

    if "$@" >/tmp/compose-services-test.out 2>/tmp/compose-services-test.err; then
        cat /tmp/compose-services-test.out
        cat /tmp/compose-services-test.err >&2
        fail "${label}: expected failure"
    fi
    pass "${label}"
}

expect_eq "$(compose_service_resolve_build_target bot)" "hololive-bot" "build alias bot"
expect_eq "$(compose_service_resolve_build_target hololive-kakao-bot-go)" "hololive-bot" "build legacy module alias"
expect_eq "$(compose_service_resolve_build_target admin-api)" "hololive-admin-api" "build alias admin-api"
expect_eq "$(compose_service_resolve_build_target alarm-worker)" "hololive-alarm-worker" "build alias alarm-worker"
expect_eq "$(compose_service_resolve_build_target youtube-producer)" "youtube-producer" "build target youtube-producer"
expect_fail "build target rejects removed dispatcher" compose_service_resolve_build_target dispatcher-go

expect_eq "$(compose_service_resolve_redeploy_target bot)" "hololive-bot" "redeploy alias bot"
expect_eq "$(compose_service_resolve_redeploy_target llm)" "llm-scheduler" "redeploy alias llm"
expect_eq "$(compose_service_resolve_redeploy_target postgres)" "holo-postgres" "redeploy alias postgres"
expect_eq "$(compose_service_resolve_redeploy_target admin)" "admin-dashboard" "redeploy alias admin"
expect_eq "$(compose_service_resolve_redeploy_target all)" "" "redeploy all sentinel"
expect_eq "$(compose_service_resolve_redeploy_target youtube-producer-c)" "youtube-producer-c" "redeploy target youtube-producer-c (main-ap)"
expect_fail "redeploy target rejects removed dispatcher" compose_service_resolve_redeploy_target dispatcher-go

expect_eq "$(compose_service_resolve_log_target bot)" "hololive-bot" "log alias bot"
expect_eq "$(compose_service_resolve_log_target youtube-producer)" "youtube-producer" "log target youtube-producer"
expect_eq "$(compose_service_resolve_log_target youtube-producer-c)" "youtube-producer-c" "log target youtube-producer-c (main-ap)"
expect_fail "log target rejects removed producer shorthand" compose_service_resolve_log_target producer
expect_eq "$(compose_service_resolve_log_target llm)" "llm-scheduler" "log alias llm"
expect_fail "log target rejects removed dispatcher" compose_service_resolve_log_target dispatcher-go

. "${ROOT_DIR}/scripts/deploy/lib/ap-host.sh"

# KR.key는 gitignore된 로컬 배포 키라 클린 체크아웃에 없다.
# conf 계약 검증에는 키 실체가 불필요하므로 tmp 키로 대체한다.
SSH_KEY="$(mktemp)"
trap 'rm -f "${SSH_KEY}"' EXIT

ap_host_load "${ROOT_DIR}" osaka || fail "osaka ap-host conf loads"
expect_eq "${AP_SERVICES[*]}" "youtube-producer-a" "osaka AP services"
expect_eq "${AP_CONTAINERS[*]}" "hololive-youtube-producer-a" "osaka AP containers"
expect_eq "${AP_PORTS[*]}" "30005" "osaka AP ports"
expect_eq "${AP_COMPOSE_FILE}" "deploy/compose/docker-compose.osaka.yml" "osaka AP compose file"
expect_eq "${AP_APPROVE_DEPLOY_VAR}" "I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY" "osaka AP deploy approval var"

ap_host_load "${ROOT_DIR}" seoul || fail "seoul ap-host conf loads"
expect_eq "${AP_SERVICES[*]}" "youtube-producer-b" "seoul AP services"
expect_eq "${AP_CONTAINERS[*]}" "hololive-youtube-producer-b" "seoul AP containers"
expect_eq "${AP_PORTS[*]}" "30015" "seoul AP ports"
expect_eq "${AP_COMPOSE_FILE}" "deploy/compose/docker-compose.seoul.yml" "seoul AP compose file"
expect_eq "${AP_APPROVE_DEPLOY_VAR}" "I_APPROVE_SEOUL_ACTIVE_ACTIVE_DEPLOY" "seoul AP deploy approval var"

expect_fail "ap-host loader rejects unknown host" ap_host_load "${ROOT_DIR}" nonexistent-host

AP_ACTIVE_ACTIVE_FILES="${ROOT_DIR}/scripts/deploy/ap-rsync-files.txt"
[[ -r "${AP_ACTIVE_ACTIVE_FILES}" ]] || fail "ap active-active files list is readable"
grep -qx 'scripts/deploy/ap-iris-h3-trust-preflight.sh' "${AP_ACTIVE_ACTIVE_FILES}" || fail "ap active-active syncs Iris H3 trust preflight"
pass "ap active-active syncs Iris H3 trust preflight"
grep -q 'ap-iris-h3-trust-preflight.sh' "${ROOT_DIR}/scripts/deploy/ap-deploy.sh" || fail "ap active-active deploy runs Iris H3 trust preflight"
pass "ap active-active deploy runs Iris H3 trust preflight"
for ap_script in scripts/logs/ap-smoke.sh scripts/logs/ap-status.sh; do
    grep -q '/run/hololive-bot/ap-compose.env' "${ROOT_DIR}/${ap_script}" || fail "${ap_script} uses AP compose env"
    if grep -q '/run/hololive-bot/env' "${ROOT_DIR}/${ap_script}"; then
        fail "${ap_script} must not require legacy monolithic env"
    fi
done
pass "ap active-active smoke/status use AP compose env"
grep -q 'AP prechange compose config skipped' "${ROOT_DIR}/scripts/deploy/ap-deploy.sh" || fail "ap deploy allows token-free transition prechange config only with explicit marker"
grep -Fq "grep -Eq 'IRIS_(WEBHOOK|BOT)_TOKEN|/run/hololive-bot/(bot|alarm-worker)\\.env'" "${ROOT_DIR}/scripts/deploy/ap-deploy.sh" || fail "ap deploy prechange config bypass is limited to AP token/env-file transition"
pass "ap active-active deploy handles token-free prechange transition"
for ap_runtime_script in scripts/deploy/ap-iris-h3-trust-preflight.sh scripts/deploy/ap-completion-check.sh; do
    grep -q 'AP_REQUIRED_UDP_BUFFER_BYTES' "${ROOT_DIR}/${ap_runtime_script}" || fail "${ap_runtime_script} exposes AP_REQUIRED_UDP_BUFFER_BYTES"
    grep -q 'net.core.rmem_max' "${ROOT_DIR}/${ap_runtime_script}" || fail "${ap_runtime_script} checks net.core.rmem_max"
    grep -q 'net.core.wmem_max' "${ROOT_DIR}/${ap_runtime_script}" || fail "${ap_runtime_script} checks net.core.wmem_max"
done
pass "ap active-active verifies QUIC UDP buffer sysctls"
for ap_compose in deploy/compose/docker-compose.osaka.yml deploy/compose/docker-compose.seoul.yml; do
    grep -qx "${ap_compose}" "${AP_ACTIVE_ACTIVE_FILES}" || fail "ap active-active syncs ${ap_compose}"
done
pass "ap active-active syncs per-host compose files"
for ap_conf in scripts/deploy/lib/ap-host.sh scripts/deploy/ap-hosts/osaka.conf scripts/deploy/ap-hosts/seoul.conf; do
    grep -qx "${ap_conf}" "${AP_ACTIVE_ACTIVE_FILES}" || fail "ap active-active syncs ${ap_conf}"
done
pass "ap active-active syncs host conf and loader"
while IFS= read -r path; do
    [[ -n "${path}" ]] || continue
    [[ -e "${ROOT_DIR}/${path}" ]] || fail "ap active-active files list path exists: ${path}"
    case "${path}" in
        hololive/hololive-youtube-producer/go.sum|hololive/hololive-shared/go.sum|shared-go/go.sum|../shared-go/go.sum) ;;
        go.sum|*/go.sum) fail "ap active-active files list excludes unapproved go.sum path: ${path}" ;;
    esac
    case "${path}" in
        hololive/hololive-shared/pkg/domain/internal/model/data/*) ;;
        data|data/*|*/data/*) fail "ap active-active files list excludes unapproved data path: ${path}" ;;
    esac
done < "${AP_ACTIVE_ACTIVE_FILES}"
pass "ap active-active files list paths exist"

if grep -En '(^|/)(\.env[^/]*|[^/]*\.key|[^/]*\.pem|hololive-alarm-worker|[^/]*_test\.go|docs|logs|runtime-config|backups|artifacts)(/|$)' "${AP_ACTIVE_ACTIVE_FILES}"; then
    fail "ap active-active files list excludes forbidden deployment scope"
fi
pass "ap active-active files list excludes forbidden deployment scope"

expect_fail "osaka active-active apply requires explicit env approval" "${ROOT_DIR}/scripts/deploy/ap-deploy.sh" osaka --apply
expect_fail "seoul active-active apply requires explicit env approval" "${ROOT_DIR}/scripts/deploy/ap-deploy.sh" seoul --apply
expect_fail "osaka active-active rollback requires explicit env approval" "${ROOT_DIR}/scripts/deploy/ap-rollback.sh" osaka --apply
expect_fail "seoul active-active rollback requires explicit env approval" "${ROOT_DIR}/scripts/deploy/ap-rollback.sh" seoul --apply
