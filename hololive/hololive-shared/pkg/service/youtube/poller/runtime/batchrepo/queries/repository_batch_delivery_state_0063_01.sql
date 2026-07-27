
		)
		SELECT o.id, o.kind, o.content_id
		FROM youtube_notification_outbox o
		JOIN input i ON o.kind = i.kind AND o.content_id = i.content_id
		WHERE o.status = ?