
		SELECT o.kind AS kind, o.content_id AS content_id
		FROM youtube_notification_delivery AS d
		JOIN youtube_notification_outbox o ON o.id = d.outbox_id
		WHERE 