
		ON CONFLICT (outbox_id, room_id) DO NOTHING
	