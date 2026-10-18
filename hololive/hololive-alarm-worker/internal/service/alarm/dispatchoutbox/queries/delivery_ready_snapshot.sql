SELECT COUNT(d.id),
       COALESCE(GREATEST(EXTRACT(EPOCH FROM (NOW() - MIN(d.next_attempt_at))), 0), 0)
FROM alarm_dispatch_deliveries d
WHERE /* ready_predicate */
