
SELECT status, COUNT(*) AS rows
FROM alarm_dispatch_deliveries
WHERE status IN ('pending', 'retry', 'leased', 'sending')
GROUP BY status