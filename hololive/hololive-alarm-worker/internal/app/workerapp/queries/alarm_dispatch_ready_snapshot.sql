SELECT COUNT(id),
       COALESCE(GREATEST(EXTRACT(EPOCH FROM (clock_timestamp() - MIN(created_at))), 0), 0)
FROM alarm_dispatch_deliveries
WHERE status IN ('pending', 'retry')
  AND next_attempt_at <= clock_timestamp()
