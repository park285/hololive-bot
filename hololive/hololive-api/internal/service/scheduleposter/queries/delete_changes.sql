DELETE FROM schedule_poster_changes c
USING schedule_posters p
WHERE p.id = $2 AND c.post_id = $1 AND c.channel_id = p.channel_id
