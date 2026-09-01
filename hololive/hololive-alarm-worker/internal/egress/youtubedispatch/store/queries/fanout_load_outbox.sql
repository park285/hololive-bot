SELECT status, attempt_count, next_attempt_at, locked_at, sent_at, terminal_at, COALESCE(error, '')
FROM youtube_notification_outbox
WHERE id = $1
FOR UPDATE;
