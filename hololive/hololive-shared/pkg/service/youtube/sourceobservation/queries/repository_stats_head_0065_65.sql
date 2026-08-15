SELECT channel_id, last_resolved_scheduled_for,
       last_resolved_subscriber_count, last_resolved_view_count, last_resolved_video_count,
       prior_resolved_scheduled_for, prior_resolved_subscriber_count,
       prior_resolved_view_count, prior_resolved_video_count,
       unresolved_scheduled_for
FROM youtube_channel_stats_heads
WHERE channel_id = $1
FOR UPDATE
