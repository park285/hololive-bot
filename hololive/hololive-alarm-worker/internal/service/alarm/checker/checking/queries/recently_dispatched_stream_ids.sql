
		SELECT DISTINCT e.stream_id
		FROM unnest($2::text[], $3::timestamptz[]) AS w(stream_id, since)
		JOIN alarm_dispatch_events AS e ON e.stream_id = w.stream_id
		WHERE e.alarm_type = $1::alarm_type
		  AND e.created_at >= w.since
	