UPDATE youtube_notification_outbox
SET locked_at = NULL
WHERE id = $1
  AND status = $2
  AND locked_at = $3;
