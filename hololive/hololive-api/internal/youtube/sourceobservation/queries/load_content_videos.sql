SELECT video.video_id, video.channel_id, video.title, video.published_at, video.is_short
FROM youtube_videos AS video
WHERE video.channel_id = $1
  AND video.is_short = $2
  AND (
      video.video_id = ANY($3::text[])
      OR EXISTS (
          SELECT 1
          FROM youtube_content_evidence_clocks AS clock
          WHERE clock.video_id = video.video_id
      )
  )
ORDER BY video.video_id
FOR UPDATE OF video
