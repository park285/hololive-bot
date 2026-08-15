INSERT INTO youtube_channel_stats_evidence (
    channel_id, scheduled_for, provider, observation_id,
    subscriber_count, view_count, video_count,
    subscriber_covered, view_covered, video_covered,
    effective_at, received_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (channel_id, scheduled_for, provider) DO UPDATE SET
    observation_id = excluded.observation_id,
    subscriber_count = excluded.subscriber_count,
    view_count = excluded.view_count,
    video_count = excluded.video_count,
    subscriber_covered = excluded.subscriber_covered,
    view_covered = excluded.view_covered,
    video_covered = excluded.video_covered,
    effective_at = excluded.effective_at,
    received_at = excluded.received_at
WHERE (youtube_channel_stats_evidence.observation_id,
       youtube_channel_stats_evidence.subscriber_count,
       youtube_channel_stats_evidence.view_count,
       youtube_channel_stats_evidence.video_count,
       youtube_channel_stats_evidence.subscriber_covered,
       youtube_channel_stats_evidence.view_covered,
       youtube_channel_stats_evidence.video_covered,
       youtube_channel_stats_evidence.effective_at,
       youtube_channel_stats_evidence.received_at)
IS DISTINCT FROM (excluded.observation_id,
                  excluded.subscriber_count,
                  excluded.view_count,
                  excluded.video_count,
                  excluded.subscriber_covered,
                  excluded.view_covered,
                  excluded.video_covered,
                  excluded.effective_at,
                  excluded.received_at)
