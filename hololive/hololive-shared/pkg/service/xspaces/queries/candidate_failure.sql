UPDATE x_space_session SET candidate_error = $2, next_check_at = $3, updated_at = now()
WHERE id = 1 AND revision = $1 AND candidate_ciphertext IS NOT NULL
