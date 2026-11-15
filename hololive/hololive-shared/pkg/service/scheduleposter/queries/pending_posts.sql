-- 호출자가 읽은 행 묶음($1 포스터, $2 rows_extracted_at)이 지금도 게시 중일 때만 고른다.
-- 재추출을 기다리는 동안은 판별할 글만 고른다. 대응을 미룬 글이 상한을 채워 판별을 막지 않게 하기 위해서다.
SELECT d.post_id, d.posted_at, d.body, d.status, d.created_at, d.matched_poster_id, d.matched_rows_at
FROM schedule_posters p
JOIN schedule_poster_posts d ON d.channel_id = p.channel_id
WHERE p.id = $1 AND p.review_status = 'confirmed' AND p.rows_extracted_at = $2
  AND /* post_window */
  AND (d.status = 'unjudged'
       OR p.rows_status <> 'pending'
          AND (d.status = 'relevant'
               OR d.status IN ('matched', 'match_failed')
                  AND (d.matched_poster_id, d.matched_rows_at) IS DISTINCT FROM (p.id, p.rows_extracted_at)))
ORDER BY d.posted_at, d.post_id
LIMIT $3
