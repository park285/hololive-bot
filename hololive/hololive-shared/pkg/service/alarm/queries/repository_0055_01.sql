
		INSERT INTO alarms (room_id, user_id, channel_id, member_name, room_name, user_name, alarm_types, host_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (room_id, channel_id, host_id) DO UPDATE
		SET member_name = COALESCE(NULLIF(EXCLUDED.member_name, ''), alarms.member_name),
		    room_name = COALESCE(NULLIF(EXCLUDED.room_name, ''), alarms.room_name),
		    user_name = COALESCE(NULLIF(EXCLUDED.user_name, ''), alarms.user_name),
		    user_id = COALESCE(NULLIF(EXCLUDED.user_id, ''), alarms.user_id),
		    alarm_types = EXCLUDED.alarm_types

