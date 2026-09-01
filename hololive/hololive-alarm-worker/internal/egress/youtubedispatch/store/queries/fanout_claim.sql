WITH claim AS (
    SELECT outbox.id
    FROM youtube_notification_outbox AS outbox
    WHERE outbox.status = $1
      AND (outbox.locked_at IS NULL OR outbox.locked_at < $2)
      AND outbox.next_attempt_at <= $3
      AND outbox.created_at >= $4
      AND NOT EXISTS (
          SELECT 1
          FROM youtube_notification_delivery AS delivery
          WHERE delivery.outbox_id = outbox.id
      )
    ORDER BY outbox.next_attempt_at, outbox.created_at, outbox.id
    LIMIT $5
    FOR UPDATE OF outbox SKIP LOCKED
), updated AS (
    UPDATE youtube_notification_outbox AS outbox
    SET locked_at = $6
    FROM claim
    WHERE outbox.id = claim.id
      AND outbox.status = $1
    RETURNING outbox.id,
              outbox.kind,
              outbox.channel_id,
              outbox.content_id,
              outbox.payload::text AS payload,
              outbox.status,
              outbox.attempt_count,
              outbox.next_attempt_at,
              outbox.created_at,
              outbox.locked_at,
              outbox.sent_at,
              COALESCE(outbox.error, '') AS error
)
SELECT id, kind, channel_id, content_id, payload, status, attempt_count,
       next_attempt_at, created_at, locked_at, sent_at, error
FROM updated
ORDER BY next_attempt_at, created_at, id;
