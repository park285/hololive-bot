
		SELECT outbox_id, status, COUNT(*) AS count
		FROM youtube_notification_delivery
		WHERE 