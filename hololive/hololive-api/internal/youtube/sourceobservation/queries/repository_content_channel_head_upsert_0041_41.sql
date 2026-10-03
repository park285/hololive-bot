-- complete 근거와 기준 목록 시각은 각각 NULL을 덮지 않고 더 이른 시각만 남긴다.
INSERT INTO youtube_content_channel_heads (
    channel_id,
    observation_kind,
    earliest_complete_effective_at,
    earliest_baseline_effective_at
) VALUES ($1, $2, $3, $4)
ON CONFLICT (channel_id, observation_kind) DO UPDATE
SET earliest_complete_effective_at = CASE
        WHEN youtube_content_channel_heads.earliest_complete_effective_at IS NULL THEN EXCLUDED.earliest_complete_effective_at
        WHEN EXCLUDED.earliest_complete_effective_at IS NULL THEN youtube_content_channel_heads.earliest_complete_effective_at
        WHEN EXCLUDED.earliest_complete_effective_at < youtube_content_channel_heads.earliest_complete_effective_at THEN EXCLUDED.earliest_complete_effective_at
        ELSE youtube_content_channel_heads.earliest_complete_effective_at
    END,
    earliest_baseline_effective_at = CASE
        WHEN youtube_content_channel_heads.earliest_baseline_effective_at IS NULL THEN EXCLUDED.earliest_baseline_effective_at
        WHEN EXCLUDED.earliest_baseline_effective_at IS NULL THEN youtube_content_channel_heads.earliest_baseline_effective_at
        WHEN EXCLUDED.earliest_baseline_effective_at < youtube_content_channel_heads.earliest_baseline_effective_at THEN EXCLUDED.earliest_baseline_effective_at
        ELSE youtube_content_channel_heads.earliest_baseline_effective_at
    END,
    updated_at = NOW()
