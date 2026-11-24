SELECT o.id, o.kind, o.channel_id, o.content_id, o.payload::text AS payload, o.status, o.attempt_count, o.next_attempt_at, o.created_at, o.locked_at, o.sent_at, COALESCE(o.error, '') AS error
FROM youtube_notification_outbox AS o
WHERE o.id = ANY($1::bigint[])
  AND o.kind = ANY($2::text[])
  AND o.status = ANY($3::text[])
ORDER BY o.id ASC
