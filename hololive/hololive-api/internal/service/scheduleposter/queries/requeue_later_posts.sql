UPDATE schedule_poster_posts d
SET status = 'relevant', reason = '', matched_poster_id = NULL, matched_rows_at = NULL
FROM schedule_poster_posts a
WHERE a.post_id = $1 AND a.channel_id = d.channel_id AND d.matched_poster_id = $2 AND d.matched_rows_at = $3
  AND d.status IN ('matched', 'match_failed') AND (d.posted_at, d.post_id) > (a.posted_at, a.post_id)
