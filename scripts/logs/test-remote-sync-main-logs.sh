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
source "${ROOT_DIR}/scripts/logs/remote-sync-main-logs.sh" >/dev/null

[[ "$(remote_log_service_name osaka youtube-producer)" == "youtube-producer" ]] ||
  fail "osaka default producer should keep youtube-producer.log"
[[ "$(remote_log_service_name seoul youtube-producer-b)" == "youtube-producer-b" ]] ||
  fail "seoul producer-b should mirror youtube-producer-b.log"

pass "remote sync keeps split producer log filenames distinct"
