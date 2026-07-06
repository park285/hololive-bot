
		SELECT id, email, password_hash, display_name, avatar_url, created_at, updated_at
		FROM auth_users
		WHERE id = $1
	