
			WITH input AS (
				SELECT *
				FROM jsonb_to_recordset($1::jsonb) AS x(
					existing_event_id BIGINT,
					event_key TEXT,
					existing_payload_hash TEXT,
					incoming_payload_hash TEXT,
					alarm_type TEXT,
				channel_id TEXT,
				stream_id TEXT,
				category TEXT,
				payload JSONB
			)
		)
		INSERT INTO alarm_dispatch_event_collisions (
			existing_event_id, event_key, existing_payload_hash, incoming_payload_hash,
			alarm_type, channel_id, stream_id, category, payload_schema_version, payload
		)
		SELECT existing_event_id, event_key, existing_payload_hash, incoming_payload_hash,
			alarm_type::alarm_type, channel_id, stream_id, category, 1, payload
		FROM input
		ON CONFLICT (event_key, incoming_payload_hash) DO UPDATE SET
			existing_event_id = COALESCE(EXCLUDED.existing_event_id, alarm_dispatch_event_collisions.existing_event_id),
			existing_payload_hash = EXCLUDED.existing_payload_hash,
			alarm_type = EXCLUDED.alarm_type,
			channel_id = EXCLUDED.channel_id,
			stream_id = EXCLUDED.stream_id,
			category = EXCLUDED.category,
			payload_schema_version = EXCLUDED.payload_schema_version,
			payload = EXCLUDED.payload,
			status = 'detected',
			last_error = 'event_key payload_hash conflict',
			updated_at = NOW()