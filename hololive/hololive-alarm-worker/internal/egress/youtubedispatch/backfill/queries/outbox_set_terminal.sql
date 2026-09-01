UPDATE youtube_notification_outbox AS outbox
SET terminal_at = input.terminal_at
FROM unnest($1::bigint[], $2::timestamptz[]) AS input(id, terminal_at)
WHERE outbox.id = input.id
  AND outbox.terminal_at IS DISTINCT FROM input.terminal_at;
