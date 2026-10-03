
		SELECT id, event_key, payload_hash, alarm_type, channel_id, stream_id, category,
			payload_schema_version, payload, created_at, updated_at
		FROM alarm_dispatch_events
		WHERE id = ANY($1)