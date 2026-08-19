SELECT COUNT(id),
       COALESCE(GREATEST(EXTRACT(EPOCH FROM (clock_timestamp() - MIN(created_at))), 0), 0)
FROM notification_delivery_outbox
WHERE status = 'PENDING'
  AND next_attempt_at <= clock_timestamp()
  AND (
      locked_at IS NULL
      OR lock_expires_at <= clock_timestamp()
      OR (
          lock_expires_at IS NULL
          AND locked_at < clock_timestamp() - ($1::bigint * INTERVAL '1 millisecond')
      )
  )
