#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 || "$1" != "--runtime-producers-verified" ]]; then
  echo "usage: $0 --runtime-producers-verified" >&2
  exit 2
fi

export PGOPTIONS="-c default_transaction_read_only=on"

guard="$(psql -w -X -v ON_ERROR_STOP=1 -qAt -c 'show transaction_read_only')"
if [[ "$guard" != "on" ]]; then
  echo "alarm-worker rollback preflight requires transaction_read_only=on" >&2
  exit 1
fi

column_exists="$(psql -w -X -v ON_ERROR_STOP=1 -qAt <<'SQL'
SELECT EXISTS (
  SELECT 1
  FROM information_schema.columns
  WHERE table_schema = current_schema()
    AND table_name = 'alarm_dispatch_deliveries'
    AND column_name = 'send_unit_id'
);
SQL
)"

if [[ "$column_exists" == "f" ]]; then
  echo "alarm-worker rollback preflight passed: send-unit schema is not installed"
  exit 0
fi
if [[ "$column_exists" != "t" ]]; then
  echo "alarm-worker rollback preflight received an invalid schema result" >&2
  exit 1
fi

active_rows="$(psql -w -X -v ON_ERROR_STOP=1 -qAt -F '|' <<'SQL'
SELECT status, count(*)
FROM alarm_dispatch_deliveries
WHERE send_unit_id IS NOT NULL
  AND status IN ('pending', 'retry', 'leased', 'sending')
GROUP BY status
ORDER BY status;
SQL
)"

if [[ -n "$active_rows" ]]; then
  echo "alarm-worker rollback blocked: active send-unit deliveries remain" >&2
  printf '%s\n' "$active_rows" >&2
  exit 1
fi

echo "alarm-worker rollback preflight passed: no active send-unit deliveries"
