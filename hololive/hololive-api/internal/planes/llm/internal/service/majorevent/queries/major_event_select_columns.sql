
		SELECT id, external_id, type, title, link, COALESCE(description, '') as description, COALESCE(members, '{}') as members, pub_date,
			   event_start_date, event_end_date, status, COALESCE(link_status, 'unchecked') as link_status, link_checked_at, notified_at, COALESCE(notified_week, '') as notified_week,
			   COALESCE(notified_month, '') as notified_month, created_at, updated_at
		FROM major_events
	