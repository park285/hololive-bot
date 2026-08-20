
		SELECT token_hash, user_id, expires_at, used_at, created_at
		FROM auth_password_reset_tokens
		WHERE token_hash = $1 AND used_at IS NULL AND expires_at > $2
	