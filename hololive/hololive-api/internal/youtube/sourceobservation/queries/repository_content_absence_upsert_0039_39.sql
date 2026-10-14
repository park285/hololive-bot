INSERT INTO youtube_content_absence_slots (
    channel_id,
    observation_kind,
    scheduled_for,
    observation_id,
    evidence_sha256,
    effective_at,
    received_at,
    scope_sha256,
    coverage
) VALUES (
    $1, $2, $3, $4, decode($5, 'hex'), $6, $7, decode($8, 'hex'), $9
)
ON CONFLICT (channel_id, observation_kind, scheduled_for) DO NOTHING
