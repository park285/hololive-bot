SELECT c.post_id, c.item, c.kind, c.reason, COALESCE(c.slot, 0), COALESCE(c.target_post_id, 0), COALESCE(c.target_item, 0),
       c.day, c.start_time, COALESCE(c.status, ''), c.source_text, COALESCE(c.title_ko, '')
FROM schedule_poster_changes c
WHERE c.post_id = ANY($1::bigint[])
ORDER BY c.post_id, c.item
