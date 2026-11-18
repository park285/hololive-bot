SELECT video.video_id
FROM youtube_videos AS video
WHERE video.video_id = ANY($2::text[])
  AND (video.channel_id <> $1 OR video.is_short)
ORDER BY video.video_id
