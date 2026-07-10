
		WITH picked AS (
			SELECT id FROM youtube_notification_outbox
			WHERE status IN (?, ?) AND COALESCE(sent_at, created_at) < ?
			ORDER BY COALESCE(sent_at, created_at) ASC, id ASC
			LIMIT ?
		)
		DELETE FROM youtube_notification_outbox o
		USING picked
		WHERE o.id = picked.id

