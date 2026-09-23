SELECT session_revision FROM x_space_login_attempts WHERE id = $1 AND status = 'running' FOR UPDATE
