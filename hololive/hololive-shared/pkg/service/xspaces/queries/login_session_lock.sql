SELECT revision, active_revision, state, candidate_ciphertext IS NOT NULL
FROM x_space_session WHERE id = 1 FOR UPDATE
