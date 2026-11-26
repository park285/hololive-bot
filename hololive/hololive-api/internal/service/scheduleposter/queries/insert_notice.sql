INSERT INTO schedule_poster_notices (kind, poster_id, channel_id, post_id)
SELECT $1, p.id, p.channel_id, $3::bigint
FROM schedule_posters p
WHERE p.id = $2
ON CONFLICT DO NOTHING
