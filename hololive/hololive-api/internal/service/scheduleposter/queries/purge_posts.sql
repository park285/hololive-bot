DELETE FROM schedule_poster_posts
WHERE post_id IN (
    SELECT post_id FROM schedule_poster_posts
    WHERE posted_at < $1
    ORDER BY posted_at, post_id
    LIMIT $2
)
