
		INSERT INTO major_event_subscriptions (room_id, room_name)
		VALUES ($1, $2)
		ON CONFLICT (room_id) DO UPDATE
		SET room_name = COALESCE(EXCLUDED.room_name, major_event_subscriptions.room_name)
	