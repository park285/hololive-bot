
		SELECT %s, 'birthday' AS celebration_kind,
			EXTRACT(DAY FROM birthday)::int AS celebration_day
		FROM members
		WHERE EXTRACT(MONTH FROM birthday) = $1
		  AND status = 'active'
		UNION ALL
		SELECT %s, 'anniversary' AS celebration_kind,
			EXTRACT(DAY FROM debut_date)::int AS celebration_day
		FROM members
		WHERE EXTRACT(MONTH FROM debut_date) = $1
		  AND EXTRACT(YEAR FROM debut_date) < $2
		  AND status = 'active'
		ORDER BY celebration_day, celebration_kind, id
	