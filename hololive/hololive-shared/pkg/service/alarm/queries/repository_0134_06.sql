
		SELECT id, room_id, user_id, channel_id, member_name, room_name, user_name, alarm_types, created_at
		FROM alarms
		WHERE channel_id = $1
		  AND (
		        alarm_types @> ARRAY[$2::alarm_type]
		     OR cardinality(alarm_types) = 0
		  )
		ORDER BY created_at ASC
	