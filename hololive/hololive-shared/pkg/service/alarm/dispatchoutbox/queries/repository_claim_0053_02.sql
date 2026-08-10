		WITH legacy_head AS (
			SELECT d.id
			FROM alarm_dispatch_deliveries d
			WHERE $1::INT > 0
			  AND d.send_unit_id IS NULL
			  AND d.status IN ('pending', 'retry')
			  AND d.next_attempt_at <= NOW()
			ORDER BY d.next_attempt_at ASC, d.id ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		), due_window AS MATERIALIZED (
			SELECT d.send_unit_id, d.next_attempt_at, d.id AS delivery_id
			FROM alarm_dispatch_deliveries d
			WHERE $1::INT > 0
			  AND d.send_unit_id IS NOT NULL
			  AND d.status IN ('pending', 'retry')
			  AND d.next_attempt_at <= NOW()
			  AND NOT EXISTS (SELECT 1 FROM legacy_head)
			ORDER BY d.next_attempt_at ASC, d.id ASC
			LIMIT $1::INT * $4::INT
		), unit_candidates AS (
			SELECT DISTINCT ON (send_unit_id)
				send_unit_id AS id, next_attempt_at, delivery_id
			FROM due_window
			ORDER BY send_unit_id, next_attempt_at ASC, delivery_id ASC
		), locked_units AS (
			SELECT u.id, candidate.next_attempt_at, candidate.delivery_id,
				(
					SELECT count(due.id)
					FROM alarm_dispatch_deliveries due
					WHERE due.send_unit_id = u.id
					  AND due.status IN ('pending', 'retry')
					  AND due.next_attempt_at <= NOW()
				) AS delivery_count
			FROM unit_candidates candidate
			JOIN alarm_dispatch_send_units u ON u.id = candidate.id
			ORDER BY candidate.next_attempt_at ASC, candidate.delivery_id ASC
			LIMIT $1::INT
			FOR UPDATE OF u SKIP LOCKED
		), ranked_units AS (
			SELECT id,
				row_number() OVER (ORDER BY next_attempt_at ASC, delivery_id ASC) AS ordinal,
				sum(delivery_count) OVER (ORDER BY next_attempt_at ASC, delivery_id ASC ROWS UNBOUNDED PRECEDING) AS cumulative_deliveries
			FROM locked_units
		), next_units AS (
			SELECT id
			FROM ranked_units
			WHERE cumulative_deliveries <= $1::INT OR ordinal = 1
		), picked AS (
			SELECT d.id
			FROM alarm_dispatch_deliveries d
			WHERE d.send_unit_id IN (SELECT id FROM next_units)
			  AND d.status IN ('pending', 'retry')
			  AND d.next_attempt_at <= NOW()
			UNION ALL
			SELECT id FROM legacy_head
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
				d.dispatch_group_key, d.send_unit_id, d.status, d.attempt_count, d.next_attempt_at,
				d.locked_by, d.locked_at, d.lock_expires_at, d.sending_started_at, d.sent_at,
				d.dlq_at, d.quarantined_at, d.cancelled_at, d.last_error_code, d.last_error,
				d.created_at, d.updated_at
		)
		SELECT d.id, d.event_id, d.room_id, d.dedupe_key, d.claim_keys, d.delivery_context,
			d.dispatch_group_key, d.send_unit_id, COALESCE(u.client_request_id, ''), d.status,
			d.attempt_count, d.next_attempt_at, d.locked_by, d.locked_at, d.lock_expires_at,
			d.sending_started_at, d.sent_at, d.dlq_at, d.quarantined_at, d.cancelled_at,
			d.last_error_code, d.last_error, d.created_at, d.updated_at
		FROM updated d
		LEFT JOIN alarm_dispatch_send_units u ON u.id = d.send_unit_id
		ORDER BY d.next_attempt_at ASC, d.id ASC
