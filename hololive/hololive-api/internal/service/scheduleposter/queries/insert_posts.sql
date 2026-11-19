WITH stored AS (
    INSERT INTO schedule_poster_posts AS e (post_id, channel_id, posted_at, body)
    SELECT d.post_id, p.channel_id, d.posted_at, d.body
    FROM schedule_posters p
    CROSS JOIN unnest($2::bigint[], $3::timestamptz[], $4::text[]) AS d(post_id, posted_at, body)
    WHERE p.id = $1 AND p.review_status = 'confirmed' AND /* fragment:post_window */
    ON CONFLICT (post_id, channel_id) DO UPDATE
    SET body = EXCLUDED.body, status = 'unjudged', reason = '', schedule_info = NULL, kind = NULL,
        matched_poster_id = NULL, matched_rows_at = NULL
    WHERE e.status <> 'revoked' AND char_length(EXCLUDED.body) > char_length(e.body) AND starts_with(EXCLUDED.body, e.body)
    RETURNING e.post_id, e.channel_id
), cleared AS (
    DELETE FROM schedule_poster_changes c
    USING stored
    WHERE c.post_id = stored.post_id AND c.channel_id = stored.channel_id
)
SELECT count(*) FROM stored
