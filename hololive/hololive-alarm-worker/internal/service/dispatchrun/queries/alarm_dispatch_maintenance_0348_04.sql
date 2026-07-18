
WITH picked AS (
    SELECT id
    FROM alarm_dispatch_deliveries
    WHERE status = $1
      AND %s < NOW() - ($2::int * INTERVAL '1 day')
    ORDER BY %s ASC, id ASC
    LIMIT $3
)
DELETE FROM alarm_dispatch_deliveries d
USING picked
WHERE d.id = picked.id