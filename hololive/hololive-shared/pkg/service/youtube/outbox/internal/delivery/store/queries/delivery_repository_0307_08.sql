
		SELECT outbox_id, status, COUNT(id) AS count
		FROM youtube_notification_delivery
		WHERE
