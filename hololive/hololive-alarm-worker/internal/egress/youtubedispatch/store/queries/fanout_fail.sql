UPDATE youtube_notification_outbox
SET status = $1,
    attempt_count = $2,
    next_attempt_at = $3,
    locked_at = NULL,
    sent_at = NULL,
    terminal_at = $4,
    error = $5
WHERE id = $6
  AND status = $7
  AND attempt_count = $8
  AND locked_at = $9
RETURNING id;
