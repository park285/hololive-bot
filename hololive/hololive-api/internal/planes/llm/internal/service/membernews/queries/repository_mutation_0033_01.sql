
		INSERT INTO member_news_subscriptions (room_id, room_name)
		VALUES ($1, $2)
		ON CONFLICT (room_id) DO UPDATE
		SET room_name = COALESCE(EXCLUDED.room_name, member_news_subscriptions.room_name),
		    updated_at = NOW()
	