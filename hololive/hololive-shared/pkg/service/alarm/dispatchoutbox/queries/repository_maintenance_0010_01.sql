
		WITH picked AS (
			SELECT id FROM alarm_dispatch_deliveries
			WHERE status='leased' AND lock_expires_at < NOW()
			ORDER BY lock_expires_at ASC, id ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE alarm_dispatch_deliveries d
		SET status='retry',
			next_attempt_at=NOW(),
			locked_by=NULL,
			locked_at=NULL,
			lock_expires_at=NULL,
			last_error='lease expired before external send',
			updated_at=NOW()
		FROM picked
		WHERE d.id = picked.id