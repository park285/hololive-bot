
		UPDATE auth_users
		SET password_hash = $1, updated_at = $2
		WHERE id = $3
	