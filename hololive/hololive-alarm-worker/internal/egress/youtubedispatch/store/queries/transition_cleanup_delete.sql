DELETE FROM youtube_notification_outbox
WHERE id = ANY($1::bigint[])
  AND status = ANY($2::text[])
  AND terminal_at IS NOT NULL
  AND terminal_at < $3
RETURNING id;
