INSERT INTO x_space_login_attempts (configuration_revision, session_revision, status)
VALUES ($1, $2, 'running') RETURNING id
