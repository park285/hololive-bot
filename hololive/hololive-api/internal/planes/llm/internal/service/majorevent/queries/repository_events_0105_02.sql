
		INSERT INTO major_events (external_id, type, title, link, description, members, pub_date, event_start_date, event_end_date, status, link_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	ON CONFLICT (external_id) DO UPDATE
	SET title = EXCLUDED.title,
		link = EXCLUDED.link,
		description = EXCLUDED.description,
		members = EXCLUDED.members,
		pub_date = EXCLUDED.pub_date,
		event_start_date = EXCLUDED.event_start_date,
		event_end_date = EXCLUDED.event_end_date,
		type = EXCLUDED.type,
		status = CASE
			WHEN major_events.status = 'canceled' THEN major_events.status
			WHEN major_events.status = 'ended' AND EXCLUDED.event_start_date >= CURRENT_DATE THEN 'active'
			ELSE major_events.status
		END,
		link_status = CASE
			WHEN major_events.link IS DISTINCT FROM EXCLUDED.link THEN 'unchecked'
			ELSE major_events.link_status
		END,
		link_checked_at = CASE
			WHEN major_events.link IS DISTINCT FROM EXCLUDED.link THEN NULL
			ELSE major_events.link_checked_at
		END,
		updated_at = NOW()
	RETURNING id
	