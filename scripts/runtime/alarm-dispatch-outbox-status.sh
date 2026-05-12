#!/usr/bin/env bash
set -euo pipefail

psql "${DATABASE_URL:?set DATABASE_URL}" -v ON_ERROR_STOP=1 <<'SQL'
SELECT status, count(*) AS rows
FROM alarm_dispatch_deliveries
GROUP BY status
ORDER BY status;

SELECT d.id, d.room_id, e.channel_id, e.stream_id, e.alarm_type,
       d.attempt_count, d.status, d.last_error_code, d.last_error, d.updated_at
FROM alarm_dispatch_deliveries d
JOIN alarm_dispatch_events e ON e.id = d.event_id
WHERE d.status IN ('retry', 'dlq', 'quarantined', 'sending', 'leased')
ORDER BY d.updated_at DESC
LIMIT 50;
SQL
