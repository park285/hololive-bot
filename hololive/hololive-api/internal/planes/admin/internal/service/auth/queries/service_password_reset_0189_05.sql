
		UPDATE auth_users
		SET password_hash = $1, updated_at = $2, session_generation = session_generation + 1
		WHERE id = $3
	