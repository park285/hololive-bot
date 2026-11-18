WITH target AS (
    SELECT d.post_id, d.channel_id, d.status, p.review_status
    FROM schedule_posters p
    JOIN schedule_poster_posts d ON d.channel_id = p.channel_id
    WHERE p.id = $1 AND d.post_id = $2
      AND /* fragment:post_window */
    FOR UPDATE OF d
), restored AS (
    UPDATE schedule_poster_posts d
    SET status = 'relevant'
    FROM target
    WHERE d.post_id = target.post_id AND d.channel_id = target.channel_id AND target.review_status = 'confirmed' AND target.status = 'revoked'
    RETURNING d.post_id
)
SELECT (SELECT review_status FROM schedule_posters WHERE id = $1), (SELECT status FROM target), EXISTS (SELECT 1 FROM restored)
