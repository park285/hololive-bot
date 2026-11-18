WITH picked AS (
    SELECT id
    FROM alarm_dispatch_deliveries
    WHERE status = 'cancelled'
      AND cancelled_at < NOW() - ($1::int * INTERVAL '1 day')
    ORDER BY cancelled_at ASC, id ASC
    LIMIT $2
)
DELETE FROM alarm_dispatch_deliveries d
WHERE d.id = ANY(ARRAY(SELECT id FROM picked))
  AND d.status = 'cancelled'
  AND d.cancelled_at < NOW() - ($1::int * INTERVAL '1 day')
