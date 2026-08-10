#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DB_MAINTENANCE_EXEC="${DB_MAINTENANCE_EXEC:-$ROOT_DIR/scripts/runtime/db-maintenance-exec.sh}"

DOCKER=(docker)
if [[ "$(id -u)" -ne 0 ]]; then
  DOCKER=(sudo -n docker)
fi

api_running="$("${DOCKER[@]}" inspect -f '{{.State.Running}}' hololive-api 2>/dev/null || printf 'false')"
if [[ "$api_running" != "false" ]]; then
  echo "alarm-worker rollback blocked: hololive-api must be stopped" >&2
  exit 1
fi

worker_running="$("${DOCKER[@]}" inspect -f '{{.State.Running}}' hololive-alarm-worker 2>/dev/null || true)"
if [[ "$worker_running" != "true" ]]; then
  echo "alarm-worker rollback blocked: current alarm consumer is not running" >&2
  exit 1
fi

required_flags=(
  "NOTIFICATION_SCHEDULER_ROLE=off"
  "CELEBRATION_RUNNER_ENABLED=false"
  "BIRTHDAY_STREAM_RUNNER_ENABLED=false"
  "YOUTUBE_OUTBOX_V3_HANDOFF_MODE=off"
  "YOUTUBE_OUTBOX_DISPATCHER_ENABLED=false"
  "ALARM_DISPATCH_CONSUMER_ENABLED=true"
)
for expected in "${required_flags[@]}"; do
  key="${expected%%=*}"
  matched="$("${DOCKER[@]}" inspect -f "{{range .Config.Env}}{{if eq . \"$expected\"}}true{{end}}{{end}}" hololive-alarm-worker 2>/dev/null || true)"
  if [[ "$matched" != "true" ]]; then
    echo "alarm-worker rollback blocked: required producer drain flag is not active: $key" >&2
    exit 1
  fi
done

exec "$DB_MAINTENANCE_EXEC" \
  bash /migrations/preflight-alarm-worker-rollback.sh --runtime-producers-verified
