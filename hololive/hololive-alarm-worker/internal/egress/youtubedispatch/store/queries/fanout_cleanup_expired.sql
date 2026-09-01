WITH candidates AS (
    SELECT outbox.id
    FROM youtube_notification_outbox AS outbox
    WHERE outbox.status = $1
      AND outbox.sent_at IS NULL
      AND outbox.created_at < $2
      AND (outbox.locked_at IS NULL OR outbox.locked_at < $3)
      AND NOT EXISTS (
          SELECT 1
          FROM youtube_notification_delivery AS delivery
          WHERE delivery.outbox_id = outbox.id
      )
    ORDER BY outbox.created_at, outbox.id
    LIMIT $4
    FOR UPDATE OF outbox SKIP LOCKED
)
DELETE FROM youtube_notification_outbox AS outbox
USING candidates
WHERE outbox.id = candidates.id
RETURNING outbox.id;
