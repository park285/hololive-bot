WITH claim AS (
        SELECT id FROM notification_delivery_outbox
        WHERE status = 'PENDING'
          AND next_attempt_at <= $2
          AND (
                locked_at IS NULL
             OR lock_expires_at < $2
             OR (lock_expires_at IS NULL AND locked_at < $1)
          )
        ORDER BY next_attempt_at ASC, created_at ASC, id ASC LIMIT $3
        FOR UPDATE SKIP LOCKED
    )
	    UPDATE notification_delivery_outbox o
	       SET locked_at = $2,
	           locked_by = $4,
	           lock_expires_at = $5
	    FROM claim WHERE o.id = claim.id
	    RETURNING o.id, o.kind, o.period_key, o.room_id, o.content_id, o.payload,
	              o.status, o.attempt_count, o.next_attempt_at, o.created_at,
	              o.locked_at, o.sent_at, o.error