
		SELECT id, room_id, COALESCE(room_name, ''), created_at
		FROM member_news_subscriptions
		ORDER BY created_at ASC
	