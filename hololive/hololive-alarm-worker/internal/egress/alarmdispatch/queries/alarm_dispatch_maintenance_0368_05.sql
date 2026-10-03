
WITH picked AS (
    SELECT e.id
    FROM alarm_dispatch_events e
    WHERE e.created_at < NOW() - ($1::int * INTERVAL '1 day')
      AND NOT EXISTS (
          SELECT 1 FROM alarm_dispatch_deliveries d WHERE d.event_id = e.id
      )
    ORDER BY e.created_at ASC, e.id ASC
    LIMIT $2
)
DELETE FROM alarm_dispatch_events e
USING picked
WHERE e.id = picked.id