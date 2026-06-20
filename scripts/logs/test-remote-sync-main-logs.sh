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
[[ "${OSAKA_SERVICES}" == "youtube-producer-a" ]] ||
  fail "osaka should default to youtube-producer-a"
[[ "${OSAKA2_REMOTE_LOG_DIR}" == "/var/log/hololive-bot" ]] ||
  fail "osaka2 should default to host-native log dir"
[[ "${OSAKA2_SERVICES}" == "youtube-producer-d" ]] ||
  fail "osaka2 should default to youtube-producer-d"
[[ "$(remote_log_service_name osaka youtube-producer)" == "youtube-producer" ]] ||
  fail "osaka legacy producer alias should keep youtube-producer.log"
[[ "$(remote_log_service_name osaka youtube-producer-a)" == "youtube-producer-a" ]] ||
  fail "osaka producer-a should mirror youtube-producer-a.log"
[[ "$(remote_log_service_name osaka2 youtube-producer-d)" == "youtube-producer-d" ]] ||
  fail "osaka2 producer-d should mirror youtube-producer-d.log"
[[ "$(remote_log_service_name seoul youtube-producer-b)" == "youtube-producer-b" ]] ||
  fail "seoul producer-b should mirror youtube-producer-b.log"
remote_log_include_patterns osaka youtube-producer-a | grep -Fx "youtube-producer-a.log.*" >/dev/null ||
  fail "osaka should include sibling logrotate files"
remote_log_include_patterns osaka youtube-producer-a | grep -Fx "archive/youtube-producer-a*" >/dev/null ||
  fail "osaka should include archived logrotate files"
remote_log_include_patterns osaka2 youtube-producer-d | grep -Fx "youtube-producer-d.log.*" >/dev/null ||
  fail "osaka2 should include sibling logrotate files"
remote_log_include_patterns osaka2 youtube-producer-d | grep -Fx "archive/youtube-producer-d*" >/dev/null ||
  fail "osaka2 should include archived logrotate files"
grep -F "olddir /var/log/hololive-bot/archive" "${ROOT_DIR}/scripts/deploy/ap-host-native-deploy.sh" >/dev/null ||
  fail "host-native logrotate should rotate into mirrored archive dir"

pass "remote sync keeps split producer log filenames distinct"
