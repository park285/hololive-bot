INSERT INTO youtube_content_channel_heads (
    channel_id,
    observation_kind,
    earliest_complete_effective_at
) VALUES ($1, $2, $3)
ON CONFLICT (channel_id, observation_kind) DO UPDATE
SET earliest_complete_effective_at = CASE
        WHEN youtube_content_channel_heads.earliest_complete_effective_at IS NULL THEN EXCLUDED.earliest_complete_effective_at
        WHEN EXCLUDED.earliest_complete_effective_at IS NULL THEN youtube_content_channel_heads.earliest_complete_effective_at
        WHEN EXCLUDED.earliest_complete_effective_at < youtube_content_channel_heads.earliest_complete_effective_at THEN EXCLUDED.earliest_complete_effective_at
        ELSE youtube_content_channel_heads.earliest_complete_effective_at
    END,
    updated_at = NOW()
