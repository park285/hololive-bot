#!/usr/bin/env bash
set -euo pipefail

usage="usage: alarm-dispatch-outbox-retention.sh <status> <retention-days> <limit>"

status="${1:-}"
retention_days="${2:-}"
limit="${3:-}"

if [[ -z "$status" || -z "$retention_days" || -z "$limit" ]]; then
  echo "$usage" >&2
  exit 2
fi

case "$status" in
  sent|dlq|quarantined|cancelled)
    ;;
  *)
    echo "status must be one of: sent, dlq, quarantined, cancelled" >&2
    exit 2
    ;;
esac

case "$retention_days" in
  ''|*[!0-9]*)
    echo "retention-days must be a positive integer" >&2
    exit 2
    ;;
esac

case "$limit" in
  ''|*[!0-9]*)
    echo "limit must be a positive integer" >&2
    exit 2
    ;;
esac

if [[ "$retention_days" -le 0 || "$limit" -le 0 || "$limit" -gt 10000 ]]; then
  echo "retention-days and limit must be positive; limit must be <= 10000" >&2
  exit 2
fi

if [[ -z "${DATABASE_URL:-}" ]]; then
  echo "DATABASE_URL is required" >&2
  exit 2
fi

timestamp_column="$status"
case "$status" in
  sent) timestamp_column="sent_at" ;;
  dlq) timestamp_column="dlq_at" ;;
  quarantined) timestamp_column="quarantined_at" ;;
  cancelled) timestamp_column="cancelled_at" ;;
esac

psql "$DATABASE_URL" \
  -v ON_ERROR_STOP=1 \
  -v status="$status" \
  -v timestamp_column="$timestamp_column" \
  -v retention_days="$retention_days" \
  -v limit="$limit" <<'SQL'
WITH picked AS (
    SELECT id
    FROM alarm_dispatch_deliveries
    WHERE status = :'status'
      AND CASE :'timestamp_column'
            WHEN 'sent_at' THEN sent_at
            WHEN 'dlq_at' THEN dlq_at
            WHEN 'quarantined_at' THEN quarantined_at
            WHEN 'cancelled_at' THEN cancelled_at
          END < NOW() - (:'retention_days'::INT * INTERVAL '1 day')
    ORDER BY
      CASE :'timestamp_column'
        WHEN 'sent_at' THEN sent_at
        WHEN 'dlq_at' THEN dlq_at
        WHEN 'quarantined_at' THEN quarantined_at
        WHEN 'cancelled_at' THEN cancelled_at
      END ASC,
      id ASC
    LIMIT :'limit'::INT
)
DELETE FROM alarm_dispatch_deliveries d
USING picked
WHERE d.id = picked.id
RETURNING d.id;
SQL

psql "$DATABASE_URL" \
  -v ON_ERROR_STOP=1 \
  -v retention_days="$retention_days" \
  -v limit="$limit" <<'SQL'
WITH picked AS (
    SELECT e.id
    FROM alarm_dispatch_events e
    WHERE e.created_at < NOW() - (:'retention_days'::INT * INTERVAL '1 day')
      AND NOT EXISTS (
          SELECT 1
          FROM alarm_dispatch_deliveries d
          WHERE d.event_id = e.id
      )
    ORDER BY e.created_at ASC, e.id ASC
    LIMIT :'limit'::INT
)
DELETE FROM alarm_dispatch_events e
USING picked
WHERE e.id = picked.id
RETURNING e.id;
SQL
