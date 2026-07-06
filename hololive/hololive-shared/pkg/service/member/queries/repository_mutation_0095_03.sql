
		UPDATE members
		SET is_graduated = $2,
			status = CASE WHEN $2 THEN 'graduated' ELSE 'active' END
		WHERE id = $1
	