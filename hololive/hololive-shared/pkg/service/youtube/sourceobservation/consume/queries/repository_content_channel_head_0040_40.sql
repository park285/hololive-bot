SELECT earliest_complete_effective_at
FROM youtube_content_channel_heads
WHERE channel_id = $1
  AND observation_kind = $2
FOR UPDATE
