#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

mkdir -p "$tmp/bin"
cat >"$tmp/bin/psql" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

args="$*"
if [[ "$args" == *"show transaction_read_only"* ]]; then
  printf '%s\n' "${FAKE_GUARD:-on}"
  exit 0
fi

sql="$(cat)"
if [[ "$sql" == *"information_schema.columns"* ]]; then
  printf '%s\n' "${FAKE_COLUMN_EXISTS:-t}"
  exit 0
fi
if [[ "$sql" == *"send_unit_id IS NOT NULL"* ]]; then
  printf '%s' "${FAKE_ACTIVE_ROWS:-}"
  exit 0
fi

echo "unexpected psql invocation: $args $sql" >&2
exit 99
EOF
chmod +x "$tmp/bin/psql"

if PATH="$tmp/bin:$PATH" "$root/preflight-alarm-worker-rollback.sh" >"$tmp/out" 2>"$tmp/err"; then
  echo "preflight accepted a missing runtime-producers-verified assertion" >&2
  exit 1
fi
grep -q '^usage:' "$tmp/err"

if PATH="$tmp/bin:$PATH" FAKE_GUARD=off \
  "$root/preflight-alarm-worker-rollback.sh" --runtime-producers-verified >"$tmp/out" 2>"$tmp/err"; then
  echo "preflight accepted a writable PostgreSQL session" >&2
  exit 1
fi
grep -q 'requires transaction_read_only=on' "$tmp/err"

PATH="$tmp/bin:$PATH" FAKE_COLUMN_EXISTS=f \
  "$root/preflight-alarm-worker-rollback.sh" --runtime-producers-verified >"$tmp/out"
grep -q 'send-unit schema is not installed' "$tmp/out"

if PATH="$tmp/bin:$PATH" FAKE_ACTIVE_ROWS='retry|2' \
  "$root/preflight-alarm-worker-rollback.sh" --runtime-producers-verified >"$tmp/out" 2>"$tmp/err"; then
  echo "preflight accepted active send-unit deliveries" >&2
  exit 1
fi
grep -q 'active send-unit deliveries remain' "$tmp/err"
grep -q '^retry|2$' "$tmp/err"

PATH="$tmp/bin:$PATH" "$root/preflight-alarm-worker-rollback.sh" --runtime-producers-verified >"$tmp/out"
grep -q 'no active send-unit deliveries' "$tmp/out"

echo "preflight-alarm-worker-rollback tests passed"
