SELECT last_content_id
FROM youtube_content_watermarks
WHERE channel_id = $1
  AND watermark_type = 'COMMUNITY_POST'
