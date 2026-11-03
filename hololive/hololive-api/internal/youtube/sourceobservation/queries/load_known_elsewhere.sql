-- video_list 관측 영상 중 다른 채널이나 Shorts로 이미 저장된 영상 ID만 돌려준다. 결과는 $2 배열 크기로 제한된다.
SELECT video.video_id
FROM youtube_videos AS video
WHERE video.video_id = ANY($2::text[])
  AND (video.channel_id <> $1 OR video.is_short)
ORDER BY video.video_id
