UPDATE notification_delivery_outbox
		 SET status = $1, sent_at = $2, locked_at = NULL, locked_by = NULL, lock_expires_at = NULL, sending_started_at = NULL, error = NULL
		 WHERE id = $3 AND status IN ($4, $5)
		   AND (
		         (locked_by = $6 AND (status = $5 OR lock_expires_at > $2))
		      OR (locked_by IS NULL AND status = $4 AND locked_at = $7)
		   )