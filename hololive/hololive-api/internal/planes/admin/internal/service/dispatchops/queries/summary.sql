SELECT status, count(id)::text, min(created_at)
FROM alarm_dispatch_deliveries
GROUP BY status
ORDER BY status
