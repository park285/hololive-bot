
		UPDATE auth_password_reset_tokens
		SET used_at = $1
		WHERE token_hash = $2 AND used_at IS NULL AND expires_at > $1
		RETURNING user_id
	