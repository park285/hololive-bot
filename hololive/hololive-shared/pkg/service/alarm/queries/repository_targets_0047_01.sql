
		SELECT id, room_id, user_id, channel_id, member_name, room_name, user_name, alarm_types, created_at, host_id
		FROM alarms
		WHERE channel_id = ANY($1::text[])
		  AND (
		        alarm_types @> ARRAY[$2::alarm_type]
		     OR cardinality(alarm_types) = 0
		  )
		ORDER BY channel_id ASC, created_at ASC
