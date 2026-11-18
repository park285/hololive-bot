SELECT c.post_id, c.item, d.posted_at, c.created_at, c.kind, COALESCE(c.slot, 0), COALESCE(c.target_post_id, 0),
       COALESCE(c.target_item, 0), c.day, c.start_time, COALESCE(c.status, ''), c.source_text, COALESCE(c.title_ko, ''),
       c.with_ko, c.place_ko, c.tags_ko, c.note_ko, c.edited_at
FROM schedule_poster_posts d
JOIN schedule_poster_changes c ON c.post_id = d.post_id AND c.channel_id = d.channel_id
WHERE d.matched_poster_id = $1 AND d.matched_rows_at = $2 AND d.status = 'matched'
  AND c.kind <> 'rejected'
ORDER BY d.posted_at, d.post_id, c.item
