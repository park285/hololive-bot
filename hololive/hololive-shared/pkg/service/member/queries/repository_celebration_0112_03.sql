
		SELECT %s
		FROM members
		WHERE EXTRACT(MONTH FROM debut_date) = $1
		  AND EXTRACT(DAY FROM debut_date) = $2
		  AND EXTRACT(YEAR FROM debut_date) < $3
		  AND status = 'active'
	