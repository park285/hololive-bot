SELECT id, configuration_revision, session_revision, submitted_revision, status, error_code, started_at
FROM x_space_login_attempts ORDER BY id DESC LIMIT 1
