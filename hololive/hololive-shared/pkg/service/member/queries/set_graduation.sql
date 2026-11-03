
		UPDATE members
		SET is_graduated = $2,
			status = $3
		WHERE id = $1
	