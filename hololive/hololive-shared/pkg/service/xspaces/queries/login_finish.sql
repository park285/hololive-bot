UPDATE x_space_login_attempts SET status = $2, error_code = $3, submitted_revision = $4, finished_at = now()
WHERE id = $1 AND status = 'running'
