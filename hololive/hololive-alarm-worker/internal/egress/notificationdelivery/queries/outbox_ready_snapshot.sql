WITH now_value AS MATERIALIZED (
    SELECT clock_timestamp() AS value
)
SELECT COUNT(o.id),
       COALESCE(GREATEST(EXTRACT(EPOCH FROM (
           (SELECT value FROM now_value) - MIN(CASE WHEN o.locked_at IS NULL THEN o.next_attempt_at
                                     ELSE GREATEST(o.next_attempt_at, o.lock_expires_at) END)
       )), 0), 0)
FROM notification_delivery_outbox o CROSS JOIN now_value
WHERE /* ready_predicate */
