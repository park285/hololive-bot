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
unset HOL_LOG_CENTRAL_LOG_DIR HOL_LOG_CENTRAL_SERVICES HOL_LOG_OSAKA_LOG_DIR HOL_LOG_OSAKA_SERVICES
source "${ROOT_DIR}/scripts/logs/remote-sync-main-logs.sh" >/dev/null

[[ "${CENTRAL_REMOTE_LOG_DIR}" == "/var/log/hololive-bot" ]] ||
  fail "central should default to the compose log dir"
[[ "${CENTRAL_SERVICES}" == "hololive-api alarm-worker collector-c" ]] ||
  fail "central should mirror all application runtimes"
[[ "${OSAKA_REMOTE_LOG_DIR}" == "/var/log/hololive-bot" ]] ||
  fail "osaka should default to host-native log dir"
[[ "${OSAKA_SERVICES}" == "collector-a" ]] ||
  fail "osaka should default to collector-a"
[[ "$(remote_log_service_name osaka collector-a)" == "collector-a" ]] ||
  fail "osaka collector-a should mirror collector-a.log"
[[ "$(remote_log_service_name seoul collector-b)" == "collector-b" ]] ||
  fail "seoul collector-b should mirror collector-b.log"
remote_log_include_patterns central hololive-api | grep -Fx "hololive-api.log.*" >/dev/null ||
  fail "central should include sibling logrotate files"
remote_log_include_patterns central collector-c | grep -Fx "archive/collector-c*" >/dev/null ||
  fail "central should include archived collector files"
remote_log_include_patterns osaka collector-a | grep -Fx "collector-a.log.*" >/dev/null ||
  fail "osaka should include sibling logrotate files"
remote_log_include_patterns osaka collector-a | grep -Fx "archive/collector-a*" >/dev/null ||
  fail "osaka should include archived logrotate files"
status_output="$(status_remote central)"
[[ "${status_output}" != *"main="* ]] ||
  fail "remote sync status must expose only the isolated mirror namespace"

pass "remote sync keeps mirrors isolated from live runtime logs"
