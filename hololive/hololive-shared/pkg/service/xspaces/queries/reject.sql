UPDATE x_space_session
SET candidate_ciphertext = NULL, candidate_state = 'rejected', candidate_error = $2, updated_at = now()
WHERE id = 1 AND revision = $1 AND candidate_ciphertext IS NOT NULL
