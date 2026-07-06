UPDATE notification_delivery_outbox
		 SET status = $1, error = $2, locked_at = NULL, locked_by = NULL, lock_expires_at = NULL, sending_started_at = NULL
		 WHERE id = ANY($3) AND status = $4