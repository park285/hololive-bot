#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "${ROOT_DIR}/scripts/deploy/lib/compose-env.sh"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

pass() {
    echo "[PASS] $*"
}

fail() {
    echo "[FAIL] $*" >&2
    exit 1
}

expect_fail() {
    if "$@" >"$tmpdir/out" 2>"$tmpdir/err"; then
        cat "$tmpdir/out"
        cat "$tmpdir/err" >&2
        fail "expected failure: $*"
    fi
}

env_file="$tmpdir/env"
compose_file="$tmpdir/docker-compose.yml"

cat >"$env_file" <<'EOF'
DB_PASSWORD=db-secret
CACHE_PASSWORD=cache-secret
ALARM_DISPATCH_PUBLISH_MODE=pg_first
ALARM_DISPATCH_CONSUMER_MODE=pg
YOUTUBE_SCRAPER_RUNTIME_ALLOWED=false
EOF

cat >"$compose_file" <<'EOF'
services:
  app:
    image: example
    environment:
      DB_PASSWORD: ${DB_PASSWORD:?DB_PASSWORD is required}
      CACHE_PASSWORD: ${CACHE_PASSWORD:?CACHE_PASSWORD is required}
      ALARM_DISPATCH_PUBLISH_MODE: ${ALARM_DISPATCH_PUBLISH_MODE:-pg_first}
      ALARM_DISPATCH_CONSUMER_MODE: ${ALARM_DISPATCH_CONSUMER_MODE:-pg}
      YOUTUBE_SCRAPER_RUNTIME_ALLOWED: ${YOUTUBE_SCRAPER_RUNTIME_ALLOWED:-false}
      NOTIFICATION_EGRESS_ROLE: ${NOTIFICATION_EGRESS_ROLE:-alarm-worker}
      SHARED_GO_WORKSPACE_PATH: ${SHARED_GO_WORKSPACE_PATH:-./shared-go}
EOF

compose_env_validate_file_format "$env_file"
compose_env_assert_shell_matches_all_file_keys "$env_file"
compose_env_assert_no_shell_shadow_for_compose_files "$env_file" "$compose_file"
pass "valid env passes"

mapfile -t keys < <(compose_env_list_keys_from_file "$env_file")
[[ "${keys[*]}" == "ALARM_DISPATCH_CONSUMER_MODE ALARM_DISPATCH_PUBLISH_MODE CACHE_PASSWORD DB_PASSWORD YOUTUBE_SCRAPER_RUNTIME_ALLOWED" ]] || fail "unexpected env keys: ${keys[*]}"
pass "env keys are listed"

mapfile -t interpolation_keys < <(compose_env_list_interpolation_keys_from_files "$compose_file")
[[ "${interpolation_keys[*]}" == "ALARM_DISPATCH_CONSUMER_MODE ALARM_DISPATCH_PUBLISH_MODE CACHE_PASSWORD DB_PASSWORD NOTIFICATION_EGRESS_ROLE SHARED_GO_WORKSPACE_PATH YOUTUBE_SCRAPER_RUNTIME_ALLOWED" ]] || fail "unexpected interpolation keys: ${interpolation_keys[*]}"
pass "compose interpolation keys are listed"

bad_export="$tmpdir/bad-export.env"
echo 'export DB_PASSWORD=x' >"$bad_export"
expect_fail compose_env_validate_file_format "$bad_export"
pass "leading export fails"

bad_sub="$tmpdir/bad-sub.env"
echo 'DB_PASSWORD=$(cat /secret)' >"$bad_sub"
expect_fail compose_env_validate_file_format "$bad_sub"
pass "command substitution fails"

bad_key="$tmpdir/bad-key.env"
echo 'DB-PASSWORD=x' >"$bad_key"
expect_fail compose_env_validate_file_format "$bad_key"
pass "invalid key fails"

bad_line="$tmpdir/bad-line.env"
echo 'DB_PASSWORD' >"$bad_line"
expect_fail compose_env_validate_file_format "$bad_line"
pass "missing equals fails"

bad_space="$tmpdir/bad-space.env"
echo ' DB_PASSWORD=x' >"$bad_space"
expect_fail compose_env_validate_file_format "$bad_space"
pass "key whitespace fails"

bad_control="$tmpdir/bad-control.env"
printf 'DB_PASSWORD=a\tb\n' >"$bad_control"
expect_fail compose_env_validate_file_format "$bad_control"
pass "control character fails"

bad_dup="$tmpdir/bad-dup.env"
printf 'DB_PASSWORD=a\nDB_PASSWORD=b\n' >"$bad_dup"
expect_fail compose_env_validate_file_format "$bad_dup"
pass "duplicate key fails"

expect_fail env DB_PASSWORD=wrong bash -c '. "$1"; compose_env_assert_shell_matches_all_file_keys "$2"' _ "${ROOT_DIR}/scripts/deploy/lib/compose-env.sh" "$env_file"
pass "shell mismatch for env file key fails"

expect_fail env ALARM_DISPATCH_CONSUMER_MODE=valkey bash -c '. "$1"; compose_env_assert_no_shell_shadow_for_compose_files "$2" "$3"' _ "${ROOT_DIR}/scripts/deploy/lib/compose-env.sh" "$env_file" "$compose_file"
pass "shell mismatch for compose key fails"

expect_fail env NOTIFICATION_EGRESS_ROLE=youtube-scraper bash -c '. "$1"; compose_env_assert_no_shell_shadow_for_compose_files "$2" "$3"' _ "${ROOT_DIR}/scripts/deploy/lib/compose-env.sh" "$env_file" "$compose_file"
pass "shell-only compose key fails"

SHARED_GO_WORKSPACE_PATH=/tmp/shared compose_env_assert_no_shell_shadow_for_compose_files "$env_file" "$compose_file"
pass "allowed shell control key passes"
