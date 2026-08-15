INSERT INTO youtube_channel_stats_heads (
    channel_id, last_resolved_scheduled_for,
    last_resolved_subscriber_count, last_resolved_view_count, last_resolved_video_count,
    prior_resolved_scheduled_for, prior_resolved_subscriber_count,
    prior_resolved_view_count, prior_resolved_video_count,
    unresolved_scheduled_for
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (channel_id) DO UPDATE SET
    last_resolved_scheduled_for = excluded.last_resolved_scheduled_for,
    last_resolved_subscriber_count = excluded.last_resolved_subscriber_count,
    last_resolved_view_count = excluded.last_resolved_view_count,
    last_resolved_video_count = excluded.last_resolved_video_count,
    prior_resolved_scheduled_for = excluded.prior_resolved_scheduled_for,
    prior_resolved_subscriber_count = excluded.prior_resolved_subscriber_count,
    prior_resolved_view_count = excluded.prior_resolved_view_count,
    prior_resolved_video_count = excluded.prior_resolved_video_count,
    unresolved_scheduled_for = excluded.unresolved_scheduled_for,
    updated_at = NOW()
WHERE (youtube_channel_stats_heads.last_resolved_scheduled_for,
       youtube_channel_stats_heads.last_resolved_subscriber_count,
       youtube_channel_stats_heads.last_resolved_view_count,
       youtube_channel_stats_heads.last_resolved_video_count,
       youtube_channel_stats_heads.prior_resolved_scheduled_for,
       youtube_channel_stats_heads.prior_resolved_subscriber_count,
       youtube_channel_stats_heads.prior_resolved_view_count,
       youtube_channel_stats_heads.prior_resolved_video_count,
       youtube_channel_stats_heads.unresolved_scheduled_for)
IS DISTINCT FROM (excluded.last_resolved_scheduled_for,
                  excluded.last_resolved_subscriber_count,
                  excluded.last_resolved_view_count,
                  excluded.last_resolved_video_count,
                  excluded.prior_resolved_scheduled_for,
                  excluded.prior_resolved_subscriber_count,
                  excluded.prior_resolved_view_count,
                  excluded.prior_resolved_video_count,
                  excluded.unresolved_scheduled_for)
