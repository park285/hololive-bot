#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

pass() {
  echo "[PASS] $*"
}

# shellcheck disable=SC1091
unset HOL_LOG_OSAKA_LOG_DIR HOL_LOG_OSAKA_SERVICES HOL_LOG_OSAKA2_LOG_DIR HOL_LOG_OSAKA2_SERVICES
source "${ROOT_DIR}/scripts/logs/remote-sync-main-logs.sh" >/dev/null

[[ "${OSAKA_REMOTE_LOG_DIR}" == "/var/log/hololive-bot" ]] ||
  fail "osaka should default to host-native log dir"
[[ "${OSAKA_SERVICES}" == "youtube-collector-a" ]] ||
  fail "osaka should default to youtube-collector-a"
[[ "${OSAKA2_REMOTE_LOG_DIR}" == "/var/log/hololive-bot" ]] ||
  fail "osaka2 should default to host-native log dir"
[[ "${OSAKA2_SERVICES}" == "youtube-collector-d" ]] ||
  fail "osaka2 should default to youtube-collector-d"
[[ "$(remote_log_service_name osaka youtube-collector-a)" == "youtube-collector-a" ]] ||
  fail "osaka collector-a should mirror youtube-collector-a.log"
[[ "$(remote_log_service_name osaka2 youtube-collector-d)" == "youtube-collector-d" ]] ||
  fail "osaka2 collector-d should mirror youtube-collector-d.log"
[[ "$(remote_log_service_name seoul youtube-collector-b)" == "youtube-collector-b" ]] ||
  fail "seoul collector-b should mirror youtube-collector-b.log"
remote_log_include_patterns osaka youtube-collector-a | grep -Fx "youtube-collector-a.log.*" >/dev/null ||
  fail "osaka should include sibling logrotate files"
remote_log_include_patterns osaka youtube-collector-a | grep -Fx "archive/youtube-collector-a*" >/dev/null ||
  fail "osaka should include archived logrotate files"
remote_log_include_patterns osaka2 youtube-collector-d | grep -Fx "youtube-collector-d.log.*" >/dev/null ||
  fail "osaka2 should include sibling logrotate files"
remote_log_include_patterns osaka2 youtube-collector-d | grep -Fx "archive/youtube-collector-d*" >/dev/null ||
  fail "osaka2 should include archived logrotate files"
grep -F "olddir /var/log/hololive-bot/archive" "${ROOT_DIR}/scripts/deploy/ap-host-native-deploy.sh" >/dev/null ||
  fail "host-native logrotate should rotate into mirrored archive dir"

pass "remote sync keeps split collector log filenames distinct"
