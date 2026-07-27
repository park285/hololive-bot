SELECT v.video_id
FROM youtube_videos v
WHERE v.is_short = TRUE
  AND v.video_id IN (
