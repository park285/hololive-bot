SELECT video_id, is_short, published_at, first_seen_at
FROM youtube_videos
WHERE video_id IN (
