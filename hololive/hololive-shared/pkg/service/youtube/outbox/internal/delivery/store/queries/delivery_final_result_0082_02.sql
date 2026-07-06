
		SELECT id, outbox_id, room_id, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, error
		FROM youtube_notification_delivery
		WHERE 