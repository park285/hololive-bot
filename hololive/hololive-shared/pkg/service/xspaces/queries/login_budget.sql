SELECT count(id) FILTER (WHERE started_at > now() - interval '1 hour'), count(id)
FROM x_space_login_attempts WHERE started_at > now() - interval '24 hours'
