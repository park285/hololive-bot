SELECT video_id, channel_id, title, published_at, is_short
FROM youtube_videos
WHERE channel_id = $1
  AND is_short = $2
FOR UPDATE
