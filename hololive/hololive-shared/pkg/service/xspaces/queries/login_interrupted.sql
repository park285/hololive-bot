UPDATE x_space_login_attempts SET status = 'outcome_unknown', error_code = 'interrupted', finished_at = now()
WHERE status = 'running' AND started_at < now() - interval '5 minutes'
