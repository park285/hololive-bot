DELETE FROM schedule_poster_posts
WHERE (post_id, channel_id) IN (
    SELECT post_id, channel_id FROM schedule_poster_posts
    WHERE posted_at < $1
    ORDER BY posted_at, post_id, channel_id
    LIMIT $2
)
