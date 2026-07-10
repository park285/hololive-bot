
		SELECT id, event_key, payload_hash
		FROM alarm_dispatch_events
		WHERE event_key = ANY($1::TEXT[])
		ORDER BY event_key
		FOR UPDATE