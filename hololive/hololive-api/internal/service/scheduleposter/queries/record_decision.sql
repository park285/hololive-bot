UPDATE schedule_poster_posts
SET status = $2, reason = $3, schedule_info = $4, kind = $5
WHERE post_id = $1 AND status = 'unjudged'
