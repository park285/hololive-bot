SELECT status, count(*)::text, min(created_at)
FROM alarm_dispatch_deliveries
GROUP BY status
ORDER BY status
