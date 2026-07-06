
		INSERT INTO auth_password_reset_tokens (token_hash, user_id, expires_at, used_at, created_at)
		VALUES ($1, $2, $3, $4, $5)
	