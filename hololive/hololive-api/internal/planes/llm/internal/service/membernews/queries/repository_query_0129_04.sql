
		SELECT
			id,
			type,
			COALESCE(title, ''),
			COALESCE(description, ''),
			COALESCE(members, '{}'::text[]),
			pub_date,
			event_start_date,
			COALESCE(link, '')
		FROM major_events
		WHERE status = 'active'
		  AND type IN ('news', 'event')
		  AND COALESCE(link_status, 'unchecked') NOT IN ('failed', 'blocked')
	