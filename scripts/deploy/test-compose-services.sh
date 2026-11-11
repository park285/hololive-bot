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

expect_eq "$(compose_service_resolve_log_target hololive-api)" "hololive-api" "log target hololive-api"
expect_eq "$(compose_service_resolve_log_target hololive-alarm-worker)" "hololive-alarm-worker" "log target hololive-alarm-worker"
expect_eq "$(compose_service_resolve_log_target youtube-collector)" "hololive-youtube-collector-c" "log target youtube-collector"
expect_fail "log target rejects unknown service" compose_service_resolve_log_target youtube-collector-c
