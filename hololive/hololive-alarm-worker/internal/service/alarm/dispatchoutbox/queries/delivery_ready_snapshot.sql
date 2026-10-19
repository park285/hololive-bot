WITH clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
)
SELECT COUNT(d.id),
       COALESCE(GREATEST(EXTRACT(EPOCH FROM ((SELECT clock.now FROM clock) - MIN(d.next_attempt_at))), 0), 0)
FROM alarm_dispatch_deliveries d
WHERE /* ready_predicate */
