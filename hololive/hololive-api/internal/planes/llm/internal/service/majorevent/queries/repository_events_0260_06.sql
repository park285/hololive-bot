
		UPDATE major_events AS e
		SET link_status = u.link_status,
			link_checked_at = u.link_checked_at,
			updated_at = NOW()
		FROM unnest($1::int[], $2::text[], $3::timestamptz[], $4::text[]) AS u(id, link_status, link_checked_at, link)
		WHERE e.id = u.id
			AND e.link = u.link
