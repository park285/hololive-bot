
		SELECT o.id
		FROM youtube_notification_outbox o
		WHERE o.status = ?
		  AND o.sent_at IS NULL
		  AND o.created_at >= ?
		  AND (o.locked_at IS NULL OR o.locked_at < ?)
		  AND (
			EXISTS (
			  SELECT 1 FROM youtube_notification_delivery d
			  WHERE d.outbox_id = o.id
				AND d.status = ?
			)
			OR NOT EXISTS (
			  SELECT 1 FROM youtube_notification_delivery d
			  WHERE d.outbox_id = o.id
			)
		  )
		ORDER BY o.id
		LIMIT ?
		FOR UPDATE SKIP LOCKED
	