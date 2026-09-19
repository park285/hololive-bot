SELECT revision::text, state, candidate_state, last_error, candidate_error,
       last_checked_at, last_success_at, next_check_at FROM x_space_session WHERE id = 1
