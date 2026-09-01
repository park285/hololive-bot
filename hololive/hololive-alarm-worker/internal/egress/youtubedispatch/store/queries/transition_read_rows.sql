SELECT id,
       outbox_id,
       room_id,
       status,
       attempt_count,
       next_attempt_at,
       created_at,
       locked_at,
       sent_at,
       COALESCE(error, '') AS error,
       row_version
FROM youtube_notification_delivery
WHERE id = ANY($1::bigint[])
ORDER BY id;
