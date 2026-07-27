
		SELECT DISTINCT stream_id
		FROM alarm_dispatch_events
		WHERE alarm_type = $1::alarm_type
		  AND stream_id = ANY($2)
		  AND created_at >= $3
	