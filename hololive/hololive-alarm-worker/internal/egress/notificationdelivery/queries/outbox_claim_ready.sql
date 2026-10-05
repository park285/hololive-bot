WITH now_value AS MATERIALIZED (
    SELECT clock_timestamp() AS value
), claim AS (
    SELECT o.id
    FROM notification_delivery_outbox o
    CROSS JOIN now_value
    WHERE o.status = 'PENDING'
      AND o.next_attempt_at <= now_value.value
      AND (o.locked_at IS NULL OR o.lock_expires_at <= now_value.value)
      AND NOT (o.room_id = ANY(COALESCE($4::text[], '{}'::text[])))
      AND NOT (o.id = ANY(COALESCE($5::bigint[], '{}'::bigint[])))
      AND NOT EXISTS (
          SELECT 1 FROM notification_delivery_outbox earlier
          WHERE earlier.room_id = o.room_id
            AND (earlier.status = 'SENDING'
                 OR (earlier.status = 'PENDING'
                     AND earlier.next_attempt_at <= now_value.value
                     AND (earlier.next_attempt_at, earlier.created_at, earlier.id)
                         < (o.next_attempt_at, o.created_at, o.id)))
      )
    ORDER BY o.next_attempt_at ASC, o.created_at ASC, o.id ASC
    LIMIT $1
    FOR UPDATE OF o SKIP LOCKED
)
UPDATE notification_delivery_outbox o
SET locked_at = now_value.value,
    locked_by = $2,
    lock_expires_at = now_value.value + ($3::double precision * INTERVAL '1 millisecond')
FROM claim, now_value
WHERE o.id = claim.id
RETURNING o.id, o.kind, o.period_key, o.room_id, o.content_id, o.payload,
    o.status, o.attempt_count, o.next_attempt_at, o.created_at,
    o.locked_at, o.sent_at, o.error
