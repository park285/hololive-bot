UPDATE x_space_session
SET state = $2, last_error = $3, last_checked_at = now(), next_check_at = $4,
    last_success_at = CASE WHEN $3 = '' THEN now() ELSE last_success_at END, updated_at = now()
WHERE id = 1 AND active_revision = $1 AND active_ciphertext IS NOT NULL
