-- published_rows.sql이 방금 읽은 행 묶음($1 포스터, $2 rows_extracted_at)에 대응한 글의 변경을 거절만 빼고 적용 순서로 읽는다.
-- 같음(same)도 글이 말한 값을 담아, 늦게 기록된 앞선 글이 뒤 글이 확인한 값을 뒤집지 않게 한다.
SELECT c.post_id, c.item, d.posted_at, c.created_at, c.kind, COALESCE(c.slot, 0), COALESCE(c.target_post_id, 0),
       COALESCE(c.target_item, 0), c.day, c.start_time, COALESCE(c.status, ''), c.source_text, COALESCE(c.title_ko, ''),
       c.with_ko, c.place_ko, c.tags_ko, c.note_ko, c.edited_at
FROM schedule_poster_posts d
JOIN schedule_poster_changes c ON c.post_id = d.post_id AND c.channel_id = d.channel_id
WHERE d.matched_poster_id = $1 AND d.matched_rows_at = $2 AND d.status = 'matched'
  AND c.kind <> 'rejected'
ORDER BY d.posted_at, d.post_id, c.item
