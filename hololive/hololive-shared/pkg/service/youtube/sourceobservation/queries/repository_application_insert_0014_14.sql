INSERT INTO source_observation_applications (
    observation_id,
    provider,
    observation_kind,
    subject_key,
    evidence_sha256,
    entity_kind,
    entity_key,
    decision,
    effective_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (observation_id, entity_kind, entity_key) DO NOTHING
