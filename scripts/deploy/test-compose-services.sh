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
expect_eq "$(compose_service_resolve_build_target youtube-scraper)" "youtube-scraper" "build target youtube-scraper"
expect_fail "build target rejects removed dispatcher" compose_service_resolve_build_target dispatcher-go

expect_eq "$(compose_service_resolve_redeploy_target bot)" "hololive-bot" "redeploy alias bot"
expect_eq "$(compose_service_resolve_redeploy_target llm)" "llm-scheduler" "redeploy alias llm"
expect_eq "$(compose_service_resolve_redeploy_target yt-scraper)" "youtube-scraper" "redeploy alias yt-scraper"
expect_eq "$(compose_service_resolve_redeploy_target postgres)" "holo-postgres" "redeploy alias postgres"
expect_eq "$(compose_service_resolve_redeploy_target admin)" "admin-dashboard" "redeploy alias admin"
expect_eq "$(compose_service_resolve_redeploy_target all)" "" "redeploy all sentinel"
expect_fail "redeploy target rejects removed dispatcher" compose_service_resolve_redeploy_target dispatcher-go

expect_eq "$(compose_service_resolve_log_target bot)" "hololive-bot" "log alias bot"
expect_eq "$(compose_service_resolve_log_target ingester)" "stream-ingester" "log alias ingester"
expect_eq "$(compose_service_resolve_log_target llm)" "llm-scheduler" "log alias llm"
expect_fail "log target rejects removed dispatcher" compose_service_resolve_log_target dispatcher-go

expect_eq "$(compose_service_resolve_osaka_log_targets youtube)" "youtube-scraper" "osaka log alias youtube"
expect_eq "$(compose_service_resolve_osaka_log_targets stream)" "stream-ingester" "osaka log alias stream"
expect_eq "$(compose_service_resolve_osaka_log_targets all)" $'youtube-scraper\nstream-ingester' "osaka log all targets"
expect_eq "$(compose_service_resolve_osaka_container youtube-scraper)" "hololive-youtube-scraper" "osaka youtube container"
expect_eq "$(compose_service_resolve_osaka_container stream-ingester)" "hololive-stream-ingester" "osaka stream container"
expect_eq "$(compose_service_resolve_osaka_log_file youtube-scraper)" "logs/youtube-scraper.log" "osaka youtube log file"
expect_eq "$(compose_service_resolve_osaka_log_file stream-ingester)" "logs/stream-ingester.log" "osaka stream log file"
expect_fail "osaka log target rejects removed dispatcher" compose_service_resolve_osaka_log_targets dispatcher-go
