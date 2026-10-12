SELECT created_at
FROM youtube_notification_outbox
WHERE kind = 'NEW_VIDEO'
  AND content_id = $1
  AND status <> 'FAILED'
