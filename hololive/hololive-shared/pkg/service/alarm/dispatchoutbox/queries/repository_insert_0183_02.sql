
		WITH input AS (
			SELECT event_id, room_id, dedupe_key, claim_keys, delivery_context, status
			FROM jsonb_to_recordset($1::jsonb) AS x(
				event_id BIGINT,
				room_id TEXT,
				dedupe_key TEXT,
				claim_keys JSONB,
				delivery_context JSONB,
				status TEXT
			)
		), normalized AS (
				SELECT event_id,
					room_id,
					dedupe_key,
					COALESCE(ARRAY(SELECT jsonb_array_elements_text(COALESCE(claim_keys, '[]'::jsonb))), ARRAY[]::TEXT[]) AS claim_keys,
					delivery_context,
				status
			FROM input
		), inserted AS (
		INSERT INTO alarm_dispatch_deliveries (
			event_id, room_id, dedupe_key, claim_keys, delivery_context, status, next_attempt_at
		)
				SELECT event_id, room_id, dedupe_key, claim_keys, delivery_context, status, NOW()
				FROM normalized
				ON CONFLICT (dedupe_key) DO NOTHING
				RETURNING dedupe_key
		)
		SELECT (SELECT count(dedupe_key) FROM normalized), (SELECT count(dedupe_key) FROM inserted)
