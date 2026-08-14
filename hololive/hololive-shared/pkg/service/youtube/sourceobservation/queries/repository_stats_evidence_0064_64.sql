SELECT provider, subscriber_count, view_count, video_count,
       subscriber_covered, view_covered, video_covered
FROM youtube_channel_stats_evidence
WHERE channel_id = $1
  AND scheduled_for = $2
FOR UPDATE
