
		SELECT outbox_id, sent_at
		FROM youtube_notification_delivery
		WHERE outbox_id = ANY($1::bigint[])
		  AND status = $2
		  AND sent_at IS NOT NULL
