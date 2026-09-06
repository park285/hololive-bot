INSERT INTO youtube_live_pending_ends (
    video_id, channel_id, kind, observation_id, effective_at, received_at,
    scheduled_for, ended_at, negative_eligible, scope_covers
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (video_id) DO UPDATE SET
    channel_id = EXCLUDED.channel_id,
    kind = EXCLUDED.kind,
    observation_id = EXCLUDED.observation_id,
    effective_at = EXCLUDED.effective_at,
    received_at = EXCLUDED.received_at,
    scheduled_for = EXCLUDED.scheduled_for,
    ended_at = EXCLUDED.ended_at,
    negative_eligible = EXCLUDED.negative_eligible,
    scope_covers = EXCLUDED.scope_covers
