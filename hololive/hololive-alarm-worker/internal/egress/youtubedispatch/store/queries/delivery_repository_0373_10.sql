
		SELECT d.outbox_id
		FROM youtube_notification_delivery d
		JOIN youtube_notification_outbox o ON o.id = d.outbox_id
		WHERE o.status = ?
		GROUP BY d.outbox_id
		HAVING SUM(CASE WHEN d.status IN (?, ?) THEN 1 ELSE 0 END) = 0
		ORDER BY d.outbox_id ASC
		LIMIT ?
	