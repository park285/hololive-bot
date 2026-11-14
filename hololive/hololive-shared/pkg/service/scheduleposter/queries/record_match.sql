-- 호출자가 읽은 행 묶음($2 포스터, $3 rows_extracted_at)이 지금도 게시 중이고 이 글이 그 묶음에 아직 대응하지 않았을 때만 도장을 찍는다.
-- 글의 상태와 도장도 호출자가 읽은 값($6, $7, $8)과 같아야 한다. 그 사이 다른 묶음에 대응했거나 철회한 글을 늦은 작업이 덮지 않게 하기 위해서다.
-- 1행일 때만 호출자가 같은 트랜잭션에서 옛 변경을 지우고 새 변경을 넣는다. 그 사이 거절·주 범위 수정이 커밋되면 옛 도장으로 남아 읽히지 않고
-- 다시 뽑힌 묶음에 다시 대응된다. schedule_posters는 건드리지 않는다.
UPDATE schedule_poster_posts d
SET status = $4, reason = $5, matched_poster_id = $2, matched_rows_at = $3
WHERE d.post_id = $1 AND d.status IN ('relevant', 'matched', 'match_failed') AND d.status = $6
  AND (d.matched_poster_id, d.matched_rows_at) IS NOT DISTINCT FROM ($7::bigint, $8::timestamptz)
  AND (d.matched_poster_id, d.matched_rows_at) IS DISTINCT FROM ($2::bigint, $3::timestamptz)
  AND EXISTS (
      SELECT 1 FROM schedule_posters p
      WHERE p.id = $2 AND p.channel_id = d.channel_id AND p.review_status = 'confirmed' AND p.rows_extracted_at = $3
        AND /* post_window */
  )
