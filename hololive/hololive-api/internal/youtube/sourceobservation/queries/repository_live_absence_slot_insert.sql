INSERT INTO youtube_live_absence_slots (
    observation_id, scheduled_for, evidence_sha256, effective_at, received_at, scope_sha256, coverage
)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
ON CONFLICT (observation_id) DO NOTHING
