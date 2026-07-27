
		SELECT e.stream_id, d.room_id
		FROM alarm_dispatch_events AS e
		JOIN alarm_dispatch_deliveries AS d ON d.event_id = e.id
		WHERE e.alarm_type = $1::alarm_type
		  AND e.stream_id = ANY($2)
		  AND d.status = $3
		  AND d.sent_at >= $4
	