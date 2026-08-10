		WITH input AS (
			SELECT event_id, room_id, dedupe_key, claim_keys, delivery_context,
				dispatch_group_key, send_unit_key, client_request_id, status
			FROM jsonb_to_recordset($1::jsonb) AS x(
				event_id BIGINT,
				room_id TEXT,
				dedupe_key TEXT,
				claim_keys JSONB,
				delivery_context JSONB,
				dispatch_group_key TEXT,
				send_unit_key TEXT,
				client_request_id TEXT,
				status TEXT
			)
		), normalized AS (
			SELECT event_id,
				room_id,
				dedupe_key,
				COALESCE(ARRAY(SELECT jsonb_array_elements_text(COALESCE(claim_keys, '[]'::jsonb))), ARRAY[]::TEXT[]) AS claim_keys,
				delivery_context,
				dispatch_group_key,
				send_unit_key,
				client_request_id,
				status
			FROM input
		), resolved AS (
			SELECT n.event_id, n.room_id, n.dedupe_key, n.claim_keys, n.delivery_context,
				n.dispatch_group_key, n.send_unit_key, n.client_request_id, n.status,
				u.id AS send_unit_id
			FROM normalized n
			LEFT JOIN alarm_dispatch_send_units u
			  ON u.unit_key = n.send_unit_key
			 AND u.dispatch_group_key = n.dispatch_group_key
			 AND u.room_id = n.room_id
			 AND u.client_request_id = n.client_request_id
			WHERE (n.status = 'shadowed' AND n.send_unit_key = '' AND n.dispatch_group_key = '')
			   OR u.id IS NOT NULL
		), inserted AS (
			INSERT INTO alarm_dispatch_deliveries (
				event_id, room_id, dedupe_key, claim_keys, delivery_context, dispatch_group_key, send_unit_id, status, next_attempt_at
			)
			SELECT event_id, room_id, dedupe_key, claim_keys, delivery_context, NULLIF(dispatch_group_key, ''), send_unit_id, status, NOW()
			FROM resolved
			ON CONFLICT (dedupe_key) DO UPDATE
			SET event_id = EXCLUDED.event_id,
				claim_keys = EXCLUDED.claim_keys,
				delivery_context = EXCLUDED.delivery_context,
				dispatch_group_key = EXCLUDED.dispatch_group_key,
				send_unit_id = EXCLUDED.send_unit_id,
				status = 'pending',
				next_attempt_at = NOW(),
				updated_at = NOW()
			WHERE alarm_dispatch_deliveries.status = 'shadowed'
			  AND EXCLUDED.status = 'pending'
			RETURNING dedupe_key
		)
		SELECT (SELECT count(dedupe_key) FROM resolved), (SELECT count(dedupe_key) FROM inserted)
