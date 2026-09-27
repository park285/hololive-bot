		WITH input AS (
			SELECT event_id, room_id, dedupe_key, claim_keys, delivery_context,
				dispatch_group_key, send_unit_key, client_request_id
			FROM jsonb_to_recordset($1::jsonb) AS x(
				event_id BIGINT,
				room_id TEXT,
				dedupe_key TEXT,
				claim_keys JSONB,
				delivery_context JSONB,
				dispatch_group_key TEXT,
				send_unit_key TEXT,
				client_request_id TEXT
			)
		), normalized AS (
			SELECT event_id,
				room_id,
				dedupe_key,
				COALESCE(ARRAY(SELECT jsonb_array_elements_text(COALESCE(claim_keys, '[]'::jsonb))), ARRAY[]::TEXT[]) AS claim_keys,
				delivery_context,
				dispatch_group_key,
				send_unit_key,
				client_request_id
			FROM input
		), resolved AS (
			SELECT n.event_id, n.room_id, n.dedupe_key, n.claim_keys, n.delivery_context,
				n.dispatch_group_key, u.id AS send_unit_id
			FROM normalized n
			JOIN alarm_dispatch_send_units u
			  ON u.unit_key = n.send_unit_key
			 AND u.dispatch_group_key = n.dispatch_group_key
			 AND u.room_id = n.room_id
			 AND u.client_request_id = n.client_request_id
		), inserted AS (
			INSERT INTO alarm_dispatch_deliveries (
				event_id, room_id, dedupe_key, claim_keys, delivery_context, dispatch_group_key, send_unit_id, status, next_attempt_at
			)
			SELECT event_id, room_id, dedupe_key, claim_keys, delivery_context, NULLIF(dispatch_group_key, ''), send_unit_id, 'pending', NOW()
			FROM resolved
			ON CONFLICT (dedupe_key) DO NOTHING
			RETURNING dedupe_key
		)
		SELECT (SELECT count(dedupe_key) FROM resolved), (SELECT count(dedupe_key) FROM inserted)
