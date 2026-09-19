UPDATE x_space_session
SET active_ciphertext = candidate_ciphertext, active_revision = revision,
    candidate_ciphertext = NULL, candidate_state = 'accepted', candidate_error = $2,
    state = 'connected', last_error = '', last_checked_at = now(), last_success_at = now(), updated_at = now()
WHERE id = 1 AND revision = $1 AND candidate_ciphertext IS NOT NULL
