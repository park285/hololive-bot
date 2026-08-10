#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
preflight="$root/scripts/runtime/preflight-alarm-worker-rollback.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin"

cat >"$tmp/bin/sudo" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "${1:-}" == "-n" ]] && shift
exec "$@"
EOF

cat >"$tmp/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
op="${1:-}"
shift
case "$op" in
  inspect)
    container="${!#}"
    if [[ "$*" == *'.State.Running'* ]]; then
      if [[ "$container" == "hololive-api" ]]; then
        printf '%s\n' "${FAKE_API_RUNNING:-false}"
      else
        printf '%s\n' "${FAKE_WORKER_RUNNING:-true}"
      fi
    else
      if [[ -n "${FAKE_MISSING_FLAG:-}" && "$*" == *"${FAKE_MISSING_FLAG}="* ]]; then
        printf '%s\n' false
      else
        printf '%s\n' true
      fi
    fi
    ;;
  *) exit 99 ;;
esac
EOF

cat >"$tmp/bin/db-maintenance" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >"${FAKE_DB_CAPTURE}"
EOF
chmod +x "$tmp/bin/sudo" "$tmp/bin/docker" "$tmp/bin/db-maintenance"

run_preflight() {
  PATH="$tmp/bin:$PATH" \
    DB_MAINTENANCE_EXEC="$tmp/bin/db-maintenance" \
    FAKE_DB_CAPTURE="$tmp/db-call" \
    "$preflight"
}

if FAKE_API_RUNNING=true run_preflight >"$tmp/out" 2>"$tmp/err"; then
  echo "preflight accepted a running hololive-api producer" >&2
  exit 1
fi
grep -q 'hololive-api must be stopped' "$tmp/err"

if FAKE_WORKER_RUNNING=false run_preflight >"$tmp/out" 2>"$tmp/err"; then
  echo "preflight accepted a stopped alarm consumer" >&2
  exit 1
fi
grep -q 'current alarm consumer is not running' "$tmp/err"

for key in NOTIFICATION_SCHEDULER_ROLE CELEBRATION_RUNNER_ENABLED BIRTHDAY_STREAM_RUNNER_ENABLED \
  YOUTUBE_OUTBOX_V3_HANDOFF_MODE YOUTUBE_OUTBOX_DISPATCHER_ENABLED ALARM_DISPATCH_CONSUMER_ENABLED; do
  if FAKE_MISSING_FLAG="$key" run_preflight >"$tmp/out" 2>"$tmp/err"; then
    echo "preflight accepted an invalid producer drain flag: $key" >&2
    exit 1
  fi
  grep -q "producer drain flag is not active: $key" "$tmp/err"
done

run_preflight
grep -q '^bash /migrations/preflight-alarm-worker-rollback.sh --runtime-producers-verified$' "$tmp/db-call"

echo "preflight-alarm-worker-rollback runtime tests passed"
