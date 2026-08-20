SELECT COUNT(id),
       COALESCE(GREATEST(EXTRACT(EPOCH FROM (clock_timestamp() - MIN(created_at))), 0), 0)
FROM youtube_notification_delivery
WHERE status = 'PENDING'
  AND next_attempt_at <= clock_timestamp()
  AND (
      locked_at IS NULL
      OR locked_at < clock_timestamp() - ($1::bigint * INTERVAL '1 millisecond')
  )
