WITH picked AS (
    SELECT outbox.id
    FROM youtube_notification_outbox AS outbox
    WHERE outbox.status = $1
      AND outbox.sent_at IS NULL
      AND outbox.created_at >= $2
      AND (outbox.locked_at IS NULL OR outbox.locked_at < $3)
      AND NOT EXISTS (
          SELECT 1
          FROM youtube_notification_delivery AS delivery
          WHERE delivery.outbox_id = outbox.id
      )
    ORDER BY outbox.created_at, outbox.id
    LIMIT $4
    FOR UPDATE OF outbox SKIP LOCKED
), revived AS (
    UPDATE youtube_notification_outbox AS outbox
    SET status = $5,
        attempt_count = 0,
        next_attempt_at = $6,
        locked_at = NULL,
        sent_at = NULL,
        terminal_at = NULL,
        error = ''
    FROM picked
    WHERE outbox.id = picked.id
      AND outbox.status = $1
    RETURNING outbox.id
)
SELECT id FROM revived ORDER BY id;
