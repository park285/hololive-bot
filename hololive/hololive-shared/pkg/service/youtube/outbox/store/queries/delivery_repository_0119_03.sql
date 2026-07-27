
		WITH claim AS (
			SELECT id
			FROM youtube_notification_delivery
			WHERE status = $1
			  AND (locked_at IS NULL OR locked_at < $2)
			  AND next_attempt_at <= $3
			ORDER BY next_attempt_at ASC, created_at ASC, id ASC
			LIMIT $4
			FOR UPDATE SKIP LOCKED
		)
		UPDATE youtube_notification_delivery d
		SET locked_at = $5
		FROM claim
		WHERE d.id = claim.id
		RETURNING d.id, d.outbox_id, d.room_id, d.status, d.attempt_count,
		          d.next_attempt_at, d.created_at, d.locked_at, d.sent_at, COALESCE(d.error, '') AS error
	