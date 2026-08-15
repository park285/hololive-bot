INSERT INTO youtube_live_viewer_sample_evidence (
    video_id, sample_window_start, provider, observation_id, viewer_count, availability,
    sample_window_seconds, scheduled_for, effective_at, received_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (video_id, sample_window_start, provider) DO UPDATE SET
    observation_id = excluded.observation_id,
    viewer_count = excluded.viewer_count,
    availability = excluded.availability,
    sample_window_seconds = excluded.sample_window_seconds,
    scheduled_for = excluded.scheduled_for,
    effective_at = excluded.effective_at,
    received_at = excluded.received_at
