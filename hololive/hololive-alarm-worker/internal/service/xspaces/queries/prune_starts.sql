DELETE FROM x_space_starts WHERE space_id IN (
 SELECT space_id FROM x_space_starts WHERE first_seen_at < now() - interval '30 days'
 ORDER BY first_seen_at LIMIT 100
)
