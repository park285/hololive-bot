SELECT id, status, terminal_at
FROM youtube_notification_outbox
WHERE status = ANY($1::text[])
  AND terminal_at IS NOT NULL
  AND terminal_at < $2
  AND (
      $3::timestamptz IS NULL
      OR terminal_at > $3
      OR (terminal_at = $3 AND id > $4)
  )
ORDER BY terminal_at, id
LIMIT $5
FOR UPDATE SKIP LOCKED;
