#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "${ROOT_DIR}/scripts/deploy/lib/compose-services.sh"
TEST_TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TEST_TMP_DIR}"' EXIT
TEST_OUT="${TEST_TMP_DIR}/out"
TEST_ERR="${TEST_TMP_DIR}/err"

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

    if "$@" >"${TEST_OUT}" 2>"${TEST_ERR}"; then
        cat "${TEST_OUT}"
        cat "${TEST_ERR}" >&2
        fail "${label}: expected failure"
    fi
    pass "${label}"
}

expect_fail_contains() {
    local label="$1"
    local expected="$2"
    shift 2

    if "$@" >"${TEST_OUT}" 2>"${TEST_ERR}"; then
        cat "${TEST_OUT}"
        cat "${TEST_ERR}" >&2
        fail "${label}: expected failure"
    fi
    grep -Fq "${expected}" "${TEST_ERR}" \
        || fail "${label}: missing error '${expected}'"
    pass "${label}"
}

expect_eq "$(compose_service_resolve_build_target hololive-api)" "hololive-api" "build target hololive-api"
expect_eq "$(compose_service_resolve_build_target hololive-alarm-worker)" "hololive-alarm-worker" "build target hololive-alarm-worker"
expect_eq "$(compose_service_resolve_build_target youtube-collector)" "youtube-collector" "build target youtube-collector"
expect_eq "$(compose_service_resolve_build_target youtube-po-c)" "youtube-po-c" "build target youtube-po-c"
expect_fail "build target rejects unknown service" compose_service_resolve_build_target alarm-worker

expect_eq "$(compose_service_resolve_log_target hololive-api)" "hololive-api" "log target hololive-api"
expect_eq "$(compose_service_resolve_log_target hololive-alarm-worker)" "hololive-alarm-worker" "log target hololive-alarm-worker"
expect_eq "$(compose_service_resolve_log_target youtube-collector)" "hololive-youtube-collector-c" "log target youtube-collector"
expect_fail "log target rejects unknown service" compose_service_resolve_log_target youtube-collector-c

. "${ROOT_DIR}/scripts/deploy/lib/ap-host.sh"

# 배포 키(~/.ssh/KR.key)는 운영자 호스트에만 있다.
# conf 계약 검증에는 키 실체가 불필요하므로 tmp 키로 대체한다.
SSH_KEY="${TEST_TMP_DIR}/ssh-key"
: >"${SSH_KEY}"
export SSH_KEY

ap_host_load "${ROOT_DIR}" osaka || fail "osaka ap-host conf loads"
expect_eq "${AP_SERVICES[*]}" "youtube-collector-a" "osaka AP services"
expect_eq "${AP_CONTAINERS[*]}" "hololive-youtube-collector-a" "osaka AP containers"
expect_eq "${AP_PORTS[*]}" "30005" "osaka AP ports"
expect_eq "${AP_APPROVE_DEPLOY_VAR}" "I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY" "osaka AP deploy approval var"

expect_fail "ap-host loader rejects unknown host" ap_host_load "${ROOT_DIR}" nonexistent-host
# Seoul b는 2026-11-06 iris-seoul Podman Quadlet(po-central-cutover.sh b)으로 옮겨 AP host가 아니다.
expect_fail "ap-host loader has no seoul host" ap_host_load "${ROOT_DIR}" seoul
ap_udp_lib="scripts/deploy/lib/require-quic-udp-buffer.sh"

# persisted 검증은 sysctl --system 적용 의미론(last-wins: sysctl.d lexical 순서 후 sysctl.conf 최종)을 따라야 한다.
quic_fixture_root="${TEST_TMP_DIR}/quic"
mkdir -p "${quic_fixture_root}/etc/sysctl.d"
printf 'net.core.rmem_max=2048\nnet.core.wmem_max=2048\n' > "${quic_fixture_root}/etc/sysctl.d/10-high.conf"
printf 'net.core.rmem_max=512\nnet.core.wmem_max=512\n' > "${quic_fixture_root}/etc/sysctl.d/90-low-override.conf"
if AP_SYSCTL_ROOT="${quic_fixture_root}" bash "${ROOT_DIR}/${ap_udp_lib}" 1024 fixture-host >/dev/null 2>&1; then
    fail "${ap_udp_lib} must fail when a later sysctl.d file overrides persisted buffers below the requirement (last-wins)"
fi
pass "quic udp lib rejects later-file low override (persisted last-wins)"

printf 'net.core.rmem_max=4096\nnet.core.wmem_max=4096\n' > "${quic_fixture_root}/etc/sysctl.d/90-low-override.conf"
AP_SYSCTL_ROOT="${quic_fixture_root}" bash "${ROOT_DIR}/${ap_udp_lib}" 1024 fixture-host >/dev/null 2>&1 \
    || fail "${ap_udp_lib} must pass when the effective persisted value satisfies the requirement"
pass "quic udp lib accepts sufficient effective persisted value"

printf 'net.core.rmem_max=512\nnet.core.wmem_max=512\n' > "${quic_fixture_root}/etc/sysctl.conf"
if AP_SYSCTL_ROOT="${quic_fixture_root}" bash "${ROOT_DIR}/${ap_udp_lib}" 1024 fixture-host >/dev/null 2>&1; then
    fail "${ap_udp_lib} must treat /etc/sysctl.conf as the final persisted assignment (sysctl --system order)"
fi
pass "quic udp lib applies sysctl.conf as final override"
