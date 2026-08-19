#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
. "${ROOT_DIR}/scripts/deploy/lib/kapu-alarm-worker-fence.sh"

assert_blocked() {
    if assert_kapu_alarm_worker_start_allowed kapu "$@" >/dev/null 2>&1; then
        echo "expected kapu alarm-worker start to be blocked: $*" >&2
        exit 1
    fi
}

assert_allowed() {
    assert_kapu_alarm_worker_start_allowed kapu "$@" >/dev/null
}

assert_blocked 0 up
assert_blocked 0 up -d hololive-alarm-worker
assert_blocked 0 up -d hololive-api
assert_blocked 0 up -d admin-dashboard
assert_blocked 0 run --rm hololive-api
assert_blocked 0 start
assert_blocked 0 restart hololive-alarm-worker

assert_allowed 0 up -d --no-deps hololive-api
assert_allowed 0 up -d youtube-collector
assert_allowed 0 start hololive-api
assert_allowed 0 config --quiet
assert_kapu_alarm_worker_start_allowed hololive-osaka 0 up -d hololive-alarm-worker >/dev/null
HOLOLIVE_KAPU_ALARM_WORKER_ROLLBACK_APPROVED=1 \
    assert_kapu_alarm_worker_start_allowed kapu 0 up -d hololive-alarm-worker >/dev/null

echo "ok: kapu alarm-worker start fence"
