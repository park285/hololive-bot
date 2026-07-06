
		SELECT id, event_key, payload_hash
		FROM alarm_dispatch_events
		WHERE event_key = ANY($1::TEXT[])
		FOR UPDATE