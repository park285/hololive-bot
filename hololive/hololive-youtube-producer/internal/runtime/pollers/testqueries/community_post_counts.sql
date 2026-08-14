SELECT like_count,
       comment_count
FROM youtube_community_posts
WHERE post_id = $1
