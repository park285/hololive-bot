SELECT d.post_id, d.posted_at, d.body, d.status, d.created_at, d.matched_poster_id, d.matched_rows_at
FROM schedule_posters p
JOIN schedule_poster_posts d ON d.channel_id = p.channel_id
WHERE p.id = $1 AND p.review_status = 'confirmed' AND p.rows_extracted_at = $2
  AND /* fragment:post_window */

  AND (d.status = 'unjudged'
       OR p.rows_status <> 'pending'
          AND (d.status = 'relevant'
               OR d.status IN ('matched', 'match_failed')
                  AND (d.matched_poster_id, d.matched_rows_at) IS DISTINCT FROM (p.id, p.rows_extracted_at)))
ORDER BY d.posted_at, d.post_id
LIMIT $3
