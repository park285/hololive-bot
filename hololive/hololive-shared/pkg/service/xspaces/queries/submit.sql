INSERT INTO x_space_session (id, revision, candidate_ciphertext, candidate_state)
SELECT 1, 1, $1, 'pending' WHERE $2::bigint = 0 OR EXISTS (SELECT 1 FROM x_space_session WHERE id = 1)
ON CONFLICT (id) DO UPDATE
SET revision = x_space_session.revision + 1, candidate_ciphertext = EXCLUDED.candidate_ciphertext,
    candidate_state = 'pending', candidate_error = '', next_check_at = NULL, updated_at = now()
WHERE x_space_session.revision = $2
