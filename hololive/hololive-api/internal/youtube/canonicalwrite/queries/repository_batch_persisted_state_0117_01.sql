
		)
		SELECT o.id, o.kind, o.content_id, o.sent_at
		FROM youtube_notification_outbox o
		JOIN input i ON o.kind = i.kind AND o.content_id = i.content_id