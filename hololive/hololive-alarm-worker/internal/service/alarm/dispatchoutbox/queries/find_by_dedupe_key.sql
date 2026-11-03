		SELECT d.id, d.event_id, d.room_id, d.dedupe_key, d.claim_keys, d.delivery_context,
			d.dispatch_group_key, d.send_unit_id, COALESCE(u.client_request_id, ''), d.status,
			d.attempt_count, d.next_attempt_at, d.locked_by, d.locked_at, d.lock_expires_at,
			d.sending_started_at, d.sent_at, d.dlq_at, d.quarantined_at, d.cancelled_at,
			d.last_error_code, d.last_error, d.created_at, d.updated_at
		FROM alarm_dispatch_deliveries d
		LEFT JOIN alarm_dispatch_send_units u ON u.id = d.send_unit_id
		WHERE d.dedupe_key = ANY($1)
		ORDER BY CASE WHEN d.dedupe_key = $2 THEN 0 ELSE 1 END, d.id ASC
		LIMIT 1
