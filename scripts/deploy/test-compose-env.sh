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

cat >"$env_file" <<'EOF'
DB_PASSWORD=db-secret
CACHE_PASSWORD=cache-secret
ALARM_DISPATCH_RETENTION_LIMIT=1000
ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH=1000
YOUTUBE_COLLECTOR_RUNTIME_ALLOWED=false
EOF

compose_env_validate_file_format "$env_file"
pass "valid env passes"

mapfile -t keys < <(compose_env_list_keys_from_file "$env_file")
[[ "${keys[*]}" == "ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH ALARM_DISPATCH_RETENTION_LIMIT CACHE_PASSWORD DB_PASSWORD YOUTUBE_COLLECTOR_RUNTIME_ALLOWED" ]] || fail "unexpected env keys: ${keys[*]}"
pass "env keys are listed"


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
