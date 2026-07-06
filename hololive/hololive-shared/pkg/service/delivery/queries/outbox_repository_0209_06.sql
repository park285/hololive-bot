UPDATE notification_delivery_outbox
	            SET attempt_count = attempt_count + 1,
	                error = $1,
	                status = CASE WHEN attempt_count + 1 >= $2 THEN 'FAILED' ELSE 'PENDING' END,
	                next_attempt_at = CASE WHEN attempt_count + 1 >= $2 THEN next_attempt_at ELSE $3 END,
	                locked_at = NULL,
	                locked_by = NULL,
	                lock_expires_at = NULL,
	                sending_started_at = NULL
	            WHERE id = $4 AND status IN ($5, $6)
	              AND (
	                    (locked_by = $7 AND (status = $6 OR lock_expires_at > $8))
	                 OR (locked_by IS NULL AND status = $5 AND locked_at = $9)
	              )