
		INSERT INTO alarms (room_id, user_id, channel_id, member_name, room_name, user_name, alarm_types)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (room_id, channel_id) DO UPDATE
		SET member_name = COALESCE(EXCLUDED.member_name, alarms.member_name),
		    room_name = COALESCE(EXCLUDED.room_name, alarms.room_name),
		    user_name = COALESCE(EXCLUDED.user_name, alarms.user_name),
		    user_id = EXCLUDED.user_id,
		    alarm_types = EXCLUDED.alarm_types
	