-- $1 글이 같은 행 묶음($2 포스터, $3 rows_extracted_at)에 반영 변경을 막 기록했을 때, 그 묶음에 이미 대응한 뒤 글을 다시 대응하게 한다.
-- 뒤 글의 종류와 거절 판정은 앞선 글을 반영한 상태 기준이어야 하기 때문이다. 변경은 다음 RecordMatch가 지우고 다시 쓰며,
-- 도장이 비면 published_changes.sql이 그 사이 읽지 않는다. 철회한 글은 도장이 없어 고르지 않는다.
UPDATE schedule_poster_posts d
SET status = 'relevant', reason = '', matched_poster_id = NULL, matched_rows_at = NULL
FROM schedule_poster_posts a
WHERE a.post_id = $1 AND d.matched_poster_id = $2 AND d.matched_rows_at = $3
  AND d.status IN ('matched', 'match_failed') AND (d.posted_at, d.post_id) > (a.posted_at, a.post_id)
