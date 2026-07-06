UPDATE notification_delivery_outbox
		 SET status = $1, sending_started_at = $2, lock_expires_at = $3
		 WHERE id = $4 AND status = $5 AND locked_by = $6 AND lock_expires_at > $2