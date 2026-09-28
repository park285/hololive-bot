SELECT video.video_id, video.channel_id, video.title, video.published_at, video.is_short
FROM youtube_videos AS video
WHERE video.channel_id = $1
  AND video.is_short = $2
  AND (
      -- reducer가 읽는 영상만 잠근다: 이번 관측의 영상과 부재 판정 대상(clock 보유) 영상.
      -- clock 없는 레거시 영상은 부재 판정에서 제외되고 shorts 기준 목록 판정은 0087 존재 조회가 대신한다.
      video.video_id = ANY($3::text[])
      OR EXISTS (
          SELECT 1
          FROM youtube_content_evidence_clocks AS clock
          WHERE clock.video_id = video.video_id
      )
  )
ORDER BY video.video_id
FOR UPDATE OF video
