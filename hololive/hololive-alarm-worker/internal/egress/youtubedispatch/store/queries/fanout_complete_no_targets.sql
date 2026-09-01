UPDATE youtube_notification_outbox
SET status = $1,
    locked_at = NULL,
    sent_at = $2,
    terminal_at = $2,
    error = ''
WHERE id = $3
  AND status = $4
  AND locked_at = $5;
