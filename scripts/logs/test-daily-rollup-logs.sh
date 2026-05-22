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

mkdir -p "${TMP_DIR}/logs/remote/osaka"
printf 'bot yesterday\n' > "${TMP_DIR}/logs/bot.log"
printf 'producer mirror\n' > "${TMP_DIR}/logs/remote/osaka/youtube-producer.log"
ln -s remote/osaka/youtube-producer.log "${TMP_DIR}/logs/youtube-producer.log"

LOG_ROOT="${TMP_DIR}/logs" \
LOG_ROLLUP_DATE=2026-05-20 \
"${ROOT_DIR}/scripts/logs/daily-rollup-logs.sh" once >/tmp/daily-rollup-test.out

archive="${TMP_DIR}/logs/archive/bot-2026-05-20.log.tar.gz"
[[ -f "${archive}" ]] || fail "archive was created"
[[ ! -s "${TMP_DIR}/logs/bot.log" ]] || fail "active regular log was truncated"
[[ "$(cat "${TMP_DIR}/logs/remote/osaka/youtube-producer.log")" == "producer mirror" ]] || fail "symlinked remote mirror was not modified"

content="$(tar -xOzf "${archive}" bot.log)"
[[ "${content}" == "bot yesterday" ]] || fail "archive contains original log content"

LOG_ROOT="${TMP_DIR}/logs" \
LOG_ROLLUP_DATE=2026-05-20 \
"${ROOT_DIR}/scripts/logs/daily-rollup-logs.sh" once >/tmp/daily-rollup-test-rerun.out

[[ -f "${archive}" ]] || fail "archive remains after idempotent rerun"
pass "daily rollup archives regular logs and skips symlinked mirrors"

printf 'producer regular\n' > "${TMP_DIR}/logs/producer.log"
printf 'legacy regular\n' > "${TMP_DIR}/logs/legacy.log"
LOG_ROOT="${TMP_DIR}/logs" \
LOG_ROLLUP_DATE=2026-05-21 \
LOG_ROLLUP_FILES=producer.log \
"${ROOT_DIR}/scripts/logs/daily-rollup-logs.sh" once >/tmp/daily-rollup-test-filtered.out

[[ -f "${TMP_DIR}/logs/archive/producer-2026-05-21.log.tar.gz" ]] || fail "filtered archive was created"
[[ "$(cat "${TMP_DIR}/logs/legacy.log")" == "legacy regular" ]] || fail "unmatched regular log was not modified"
pass "daily rollup can restrict target log files"
