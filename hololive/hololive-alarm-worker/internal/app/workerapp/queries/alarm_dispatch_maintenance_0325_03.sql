
SELECT
  COALESCE(MAX(EXTRACT(EPOCH FROM (NOW() - next_attempt_at))) FILTER (WHERE status = 'pending'), 0),
  COALESCE(MAX(EXTRACT(EPOCH FROM (NOW() - next_attempt_at))) FILTER (WHERE status = 'retry'), 0),
  COALESCE(MAX(EXTRACT(EPOCH FROM (NOW() - sending_started_at))) FILTER (WHERE status = 'sending'), 0)
FROM alarm_dispatch_deliveries
WHERE status IN ('pending', 'retry', 'sending')