o.status = 'PENDING'
AND o.next_attempt_at <= now_value.value
AND (o.locked_at IS NULL OR o.lock_expires_at <= now_value.value)
AND NOT EXISTS (
    SELECT 1 FROM notification_delivery_outbox earlier
    WHERE earlier.room_id = o.room_id
      AND (earlier.status = 'SENDING'
           OR (earlier.status = 'PENDING'
               AND earlier.next_attempt_at <= now_value.value
               AND (earlier.next_attempt_at, earlier.created_at, earlier.id)
                   < (o.next_attempt_at, o.created_at, o.id)))
)
