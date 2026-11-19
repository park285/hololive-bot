SELECT pg_advisory_xact_lock(hashtextextended(concat_ws(E'\\x1f', 'schedule_poster_confirm', channel_id), 0))
FROM schedule_posters
WHERE id = $1
