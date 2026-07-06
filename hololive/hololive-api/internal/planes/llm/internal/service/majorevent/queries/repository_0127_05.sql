
		SELECT id, room_id, COALESCE(room_name, '') as room_name, created_at
		FROM major_event_subscriptions
		ORDER BY created_at ASC
	