-- 글 행을 잠가 진행 중인 RecordMatch가 끝난 뒤의 상태로 판정하고, 호출자는 같은 트랜잭션의
-- 다음 문장에서 변경을 지운다. 판별 답(schedule_info, kind)은 남긴다. 확정본이 아닌 포스터의 글은 바꾸지 않는다.
WITH target AS (
    SELECT d.post_id, d.channel_id, d.status, p.review_status
    FROM schedule_posters p
    JOIN schedule_poster_posts d ON d.channel_id = p.channel_id
    WHERE p.id = $1 AND d.post_id = $2
      AND /* post_window */
    FOR UPDATE OF d
), revoked AS (
    UPDATE schedule_poster_posts d
    SET status = 'revoked', reason = '', matched_poster_id = NULL, matched_rows_at = NULL
    FROM target
    WHERE d.post_id = target.post_id AND d.channel_id = target.channel_id AND target.review_status = 'confirmed'
      AND target.status IN ('relevant', 'matched', 'match_failed')
    RETURNING d.post_id
)
SELECT (SELECT review_status FROM schedule_posters WHERE id = $1), (SELECT status FROM target), EXISTS (SELECT 1 FROM revoked)
