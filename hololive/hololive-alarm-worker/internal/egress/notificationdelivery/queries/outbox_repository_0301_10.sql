		WITH picked AS (
			SELECT id FROM notification_delivery_outbox
			WHERE status = $1
			  AND sending_started_at < clock_timestamp() - ($2::double precision * INTERVAL '1 millisecond')
			ORDER BY sending_started_at ASC, id ASC
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		)
		UPDATE notification_delivery_outbox o
		SET status = $4,
		    locked_at = NULL,
		    locked_by = NULL,
		    lock_expires_at = NULL,
		    sending_started_at = NULL,
		    error = $5
		FROM picked
		WHERE o.id = picked.id
