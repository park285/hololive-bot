UPDATE youtube_notification_outbox AS outbox
SET terminal_at = NULL
FROM unnest($1::bigint[]) AS input(id)
WHERE outbox.id = input.id
  AND outbox.terminal_at IS NOT NULL;
