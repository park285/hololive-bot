
		SELECT %s
		FROM members
		WHERE EXTRACT(MONTH FROM birthday) = $1
		  AND EXTRACT(DAY FROM birthday) = $2
		  AND status = 'active'
	