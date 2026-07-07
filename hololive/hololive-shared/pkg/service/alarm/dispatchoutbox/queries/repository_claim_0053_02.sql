
		WITH picked AS (
			SELECT id
			FROM alarm_dispatch_deliveries
			WHERE status IN ('pending', 'retry')
			  AND next_attempt_at <= NOW()
			ORDER BY next_attempt_at ASC, id ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		), updated AS (
			UPDATE alarm_dispatch_deliveries d
			SET status = 'leased',
				locked_by = $2,
				locked_at = NOW(),
				lock_expires_at = NOW() + ($3::INT * INTERVAL '1 second'),
				updated_at = NOW()
			FROM picked
			WHERE d.id = picked.id
			RETURNING d.id, d.event_id, d.room_id, d.dedupe_key, d.claim_keys, d.delivery_context,
				d.status, d.attempt_count, d.next_attempt_at, d.locked_by, d.locked_at,
				d.lock_expires_at, d.sending_started_at, d.sent_at, d.dlq_at,
				d.quarantined_at, d.cancelled_at, d.last_error, d.created_at, d.updated_at
		)
		SELECT id, event_id, room_id, dedupe_key, claim_keys, delivery_context,
			status, attempt_count, next_attempt_at, locked_by, locked_at,
			lock_expires_at, sending_started_at, sent_at, dlq_at,
			quarantined_at, cancelled_at, last_error, created_at, updated_at
		FROM updated
		ORDER BY next_attempt_at ASC, id ASC
