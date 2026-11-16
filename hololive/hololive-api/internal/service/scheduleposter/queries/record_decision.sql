UPDATE schedule_poster_posts
SET status = $3, reason = $4, schedule_info = $5, kind = $6
WHERE post_id = $1 AND channel_id = $2 AND status = 'unjudged'
