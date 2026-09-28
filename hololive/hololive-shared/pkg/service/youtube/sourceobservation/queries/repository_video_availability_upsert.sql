INSERT INTO youtube_video_availability AS latest (
    video_id, channel_id, provider, identity_confirmed, availability, method, unknown_reason,
    observation_id, evidence_sha256, scheduled_for, effective_at, observed_at, received_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (video_id) DO UPDATE SET
    channel_id = EXCLUDED.channel_id,
    provider = EXCLUDED.provider,
    identity_confirmed = EXCLUDED.identity_confirmed,
    availability = EXCLUDED.availability,
    method = EXCLUDED.method,
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
RETURNING video_id
