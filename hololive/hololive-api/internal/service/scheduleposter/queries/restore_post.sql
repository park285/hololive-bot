-- 판별 답은 철회 때 남겨 두었으므로 relevant로만 돌리고, 다음 주기에 다시 대응한다.
WITH target AS (
    SELECT d.post_id, d.status, p.review_status
    FROM schedule_posters p
    JOIN schedule_poster_posts d ON d.channel_id = p.channel_id
    WHERE p.id = $1 AND d.post_id = $2
      AND /* post_window */
    FOR UPDATE OF d
), restored AS (
    UPDATE schedule_poster_posts d
    SET status = 'relevant'
    FROM target
    WHERE d.post_id = target.post_id AND target.review_status = 'confirmed' AND target.status = 'revoked'
    RETURNING d.post_id
)
SELECT (SELECT review_status FROM schedule_posters WHERE id = $1), (SELECT status FROM target), EXISTS (SELECT 1 FROM restored)
