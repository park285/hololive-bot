
		WITH picked AS (
			SELECT id
			FROM youtube_notification_delivery
			WHERE status = $1
			  AND locked_at IS NOT NULL
			  AND locked_at < $2
			ORDER BY locked_at ASC, id ASC
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		), updated AS (
			UPDATE youtube_notification_delivery d
			SET status = $4,
			    attempt_count = attempt_count + 1,
			    locked_at = NULL,
			    error = $5
			FROM picked
			WHERE d.id = picked.id
			RETURNING d.outbox_id
		)
		SELECT outbox_id FROM updated
	