
		WITH claim AS (
			SELECT o.id
			FROM youtube_notification_outbox o
			WHERE o.status = $1
			  AND (o.locked_at IS NULL OR o.locked_at < $2)
			  AND o.next_attempt_at <= $3
			  AND o.created_at >= $6
			  AND NOT EXISTS (
				SELECT 1 FROM youtube_notification_delivery d
				WHERE d.outbox_id = o.id
			  )
			ORDER BY o.next_attempt_at ASC, o.created_at ASC, o.id ASC
			LIMIT $4
			FOR UPDATE SKIP LOCKED
		),
		updated AS (
			UPDATE youtube_notification_outbox o
			SET locked_at = $5
			FROM claim
			WHERE o.id = claim.id
				RETURNING o.id, o.kind, o.channel_id, o.content_id, o.payload::text AS payload, o.status,
				          o.attempt_count, o.next_attempt_at, o.created_at, o.locked_at, o.sent_at, COALESCE(o.error, '') AS error
			)
			SELECT id, kind, channel_id, content_id, payload, status,
		       attempt_count, next_attempt_at, created_at, locked_at, sent_at, error
		FROM updated
		ORDER BY next_attempt_at ASC, created_at ASC, id ASC
	