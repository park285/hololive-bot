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
expect_fail "redeploy target rejects removed dispatcher" compose_service_resolve_redeploy_target dispatcher-go

expect_eq "$(compose_service_resolve_log_target bot)" "hololive-bot" "log alias bot"
expect_eq "$(compose_service_resolve_log_target youtube-producer)" "youtube-producer" "log target youtube-producer"
expect_fail "log target rejects removed producer shorthand" compose_service_resolve_log_target producer
expect_eq "$(compose_service_resolve_log_target llm)" "llm-scheduler" "log alias llm"
expect_fail "log target rejects removed dispatcher" compose_service_resolve_log_target dispatcher-go

expect_eq "$(compose_service_resolve_osaka_log_targets youtube-producer)" $'youtube-producer-a\nyoutube-producer-b' "osaka log target youtube-producer"
expect_fail "osaka log target rejects removed youtube alias" compose_service_resolve_osaka_log_targets youtube
expect_fail "osaka log target rejects removed producer shorthand" compose_service_resolve_osaka_log_targets producer
expect_eq "$(compose_service_resolve_osaka_log_targets all)" $'youtube-producer-a\nyoutube-producer-b' "osaka log all active targets"
expect_eq "$(compose_service_resolve_osaka_container youtube-producer-a)" "hololive-youtube-producer-a" "osaka youtube a container"
expect_eq "$(compose_service_resolve_osaka_container youtube-producer-b)" "hololive-youtube-producer-b" "osaka youtube b container"
expect_eq "$(compose_service_resolve_osaka_container youtube-producer)" "hololive-youtube-producer-a" "osaka youtube-producer container default"
expect_eq "$(compose_service_resolve_osaka_log_file youtube-producer-a)" "logs/youtube-producer.log" "osaka youtube a log file"
expect_eq "$(compose_service_resolve_osaka_log_file youtube-producer-b)" "logs/youtube-producer.log" "osaka youtube b log file"
expect_eq "$(compose_service_resolve_osaka_log_file youtube-producer)" "logs/youtube-producer.log" "osaka youtube-producer log file default"
expect_fail "osaka log target rejects removed dispatcher" compose_service_resolve_osaka_log_targets dispatcher-go

OSAKA_ACTIVE_ACTIVE_FILES="${ROOT_DIR}/scripts/deploy/osaka-active-active-rsync-files.txt"
[[ -r "${OSAKA_ACTIVE_ACTIVE_FILES}" ]] || fail "osaka active-active files list is readable"
while IFS= read -r path; do
    [[ -n "${path}" ]] || continue
    [[ -e "${ROOT_DIR}/${path}" ]] || fail "osaka active-active files list path exists: ${path}"
    case "${path}" in
        hololive/hololive-youtube-producer/go.sum|hololive/hololive-shared/go.sum|shared-go/go.sum|../shared-go/go.sum) ;;
        go.sum|*/go.sum) fail "osaka active-active files list excludes unapproved go.sum path: ${path}" ;;
    esac
    case "${path}" in
        hololive/hololive-shared/pkg/domain/internal/model/data/*) ;;
        data|data/*|*/data/*) fail "osaka active-active files list excludes unapproved data path: ${path}" ;;
    esac
done < "${OSAKA_ACTIVE_ACTIVE_FILES}"
pass "osaka active-active files list paths exist"

if grep -En '(^|/)(\.env[^/]*|[^/]*\.key|[^/]*\.pem|hololive-alarm-worker|_test\.go|docs|logs|runtime-config|backups|artifacts)(/|$)' "${OSAKA_ACTIVE_ACTIVE_FILES}"; then
    fail "osaka active-active files list excludes forbidden deployment scope"
fi
pass "osaka active-active files list excludes forbidden deployment scope"

expect_fail "osaka active-active apply requires explicit env approval" "${ROOT_DIR}/scripts/deploy/osaka-active-active-deploy.sh" --apply
expect_fail "osaka active-active rollback requires explicit env approval" "${ROOT_DIR}/scripts/deploy/osaka-active-active-rollback.sh" --apply
