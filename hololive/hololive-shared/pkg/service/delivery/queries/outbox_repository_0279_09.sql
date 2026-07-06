DELETE FROM notification_delivery_outbox
		 WHERE status IN ($1, $2) AND COALESCE(sent_at, created_at) < $3