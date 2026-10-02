INSERT INTO youtube_channel_live_checks AS latest (
    channel_id, provider, outcome, selected_video_id, channel_identity_confirmed, unknown_reason,
    observation_id, evidence_sha256, scheduled_for, effective_at, observed_at, received_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (channel_id) DO UPDATE SET
    provider = EXCLUDED.provider,
    outcome = EXCLUDED.outcome,
    selected_video_id = EXCLUDED.selected_video_id,
    channel_identity_confirmed = EXCLUDED.channel_identity_confirmed,
    unknown_reason = EXCLUDED.unknown_reason,
    observation_id = EXCLUDED.observation_id,
    evidence_sha256 = EXCLUDED.evidence_sha256,
    scheduled_for = EXCLUDED.scheduled_for,
    effective_at = EXCLUDED.effective_at,
    observed_at = EXCLUDED.observed_at,
    received_at = EXCLUDED.received_at,
    updated_at = NOW()
-- (effective_at, observation_id)가 더 새로운 관측만 반영한다. retention으로 원시 evidence가 지워져
-- observation_id가 NULL이 된 최신값은 같은 시각의 순서를 증명할 수 없으므로 더 늦은 시각만 대체한다.
WHERE EXCLUDED.effective_at > latest.effective_at
   OR (
       EXCLUDED.effective_at = latest.effective_at
       AND latest.observation_id IS NOT NULL
       AND EXCLUDED.observation_id > latest.observation_id
   )
RETURNING channel_id
