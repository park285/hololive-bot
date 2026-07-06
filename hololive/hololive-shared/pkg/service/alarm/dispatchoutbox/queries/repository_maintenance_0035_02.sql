
		WITH picked AS (
			SELECT id FROM alarm_dispatch_deliveries
			WHERE status='sending'
			  AND sending_started_at < NOW() - ($2::INT * INTERVAL '1 second')
			ORDER BY sending_started_at ASC, id ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE alarm_dispatch_deliveries d
		SET status='quarantined',
			quarantined_at=NOW(),
			locked_by=NULL,
			locked_at=NULL,
			lock_expires_at=NULL,
			last_error='stale sending; external send outcome unknown',
			updated_at=NOW()
		FROM picked
		WHERE d.id = picked.id