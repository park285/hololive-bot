
		SELECT e.stream_id, d.room_id, d.status = $3 AS sent
		FROM unnest($2::text[], $4::timestamptz[]) AS w(stream_id, since)
		JOIN alarm_dispatch_events AS e ON e.stream_id = w.stream_id
		JOIN alarm_dispatch_deliveries AS d ON d.event_id = e.id
		WHERE e.alarm_type = $1::alarm_type
		  AND (
		    (d.status = $3 AND d.sent_at >= w.since)
		    OR (d.status = ANY($5::text[]) AND e.created_at >= w.since)
		  )
	