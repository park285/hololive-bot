
		SELECT id, event_id, room_id, dedupe_key, claim_keys, delivery_context, status,
			attempt_count, next_attempt_at, locked_by, locked_at, lock_expires_at,
			sending_started_at, sent_at, dlq_at, quarantined_at, cancelled_at,
			last_error, created_at, updated_at
		FROM alarm_dispatch_deliveries
		WHERE dedupe_key = ANY($1)
		ORDER BY CASE WHEN dedupe_key = $2 THEN 0 ELSE 1 END, id ASC
		LIMIT 1