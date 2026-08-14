UPDATE youtube_videos
SET title = $2,
    published_at = COALESCE($3, youtube_videos.published_at),
    last_seen_at = $4
WHERE video_id = $1
  AND (title, published_at) IS DISTINCT FROM ($2, $3)
