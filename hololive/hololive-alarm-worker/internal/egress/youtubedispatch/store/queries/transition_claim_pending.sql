WITH claim AS (
    SELECT delivery.id
    FROM youtube_notification_delivery AS delivery
    JOIN youtube_notification_outbox AS outbox ON outbox.id = delivery.outbox_id
    WHERE delivery.status = $1
      AND (delivery.locked_at IS NULL OR delivery.locked_at < $2)
      AND delivery.next_attempt_at <= $3
      AND outbox.created_at >= $4
    ORDER BY delivery.next_attempt_at, delivery.created_at, delivery.id
    LIMIT $5
    FOR UPDATE OF delivery SKIP LOCKED
), updated AS (
    UPDATE youtube_notification_delivery AS delivery
    SET locked_at = $6,
        row_version = delivery.row_version + 1
    FROM claim
    WHERE delivery.id = claim.id
      AND delivery.status = $1
    RETURNING delivery.id,
              delivery.outbox_id,
              delivery.room_id,
              delivery.status,
              delivery.attempt_count,
              delivery.next_attempt_at,
              delivery.created_at,
              delivery.locked_at,
              delivery.sent_at,
              COALESCE(delivery.error, '') AS error,
              delivery.row_version
)
SELECT id, outbox_id, room_id, status, attempt_count, next_attempt_at,
       created_at, locked_at, sent_at, error, row_version
FROM updated
ORDER BY next_attempt_at, created_at, id;
