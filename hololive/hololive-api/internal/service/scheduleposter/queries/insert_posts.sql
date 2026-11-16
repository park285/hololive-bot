INSERT INTO schedule_poster_posts (post_id, channel_id, posted_at, body)
SELECT d.post_id, p.channel_id, d.posted_at, d.body
FROM schedule_posters p
CROSS JOIN unnest($2::bigint[], $3::timestamptz[], $4::text[]) AS d(post_id, posted_at, body)
WHERE p.id = $1 AND p.review_status = 'confirmed' AND /* post_window */
ON CONFLICT (post_id) DO NOTHING
