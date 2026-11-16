SELECT c.post_id, c.item, c.kind, c.reason, COALESCE(c.slot, 0), COALESCE(c.target_post_id, 0), COALESCE(c.target_item, 0),
       c.day, c.start_time, COALESCE(c.status, ''), c.source_text, COALESCE(c.title_ko, '')
FROM schedule_poster_changes c
JOIN schedule_posters p ON p.channel_id = c.channel_id
WHERE p.id = $1 AND c.post_id = ANY($2::bigint[])
ORDER BY c.post_id, c.item
