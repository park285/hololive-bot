SELECT post_id
FROM youtube_community_posts
WHERE channel_id = $1
  AND post_id = ANY($2::text[])
ORDER BY post_id
