INSERT INTO source_observations (
    provider,
    observation_kind,
    subject_key,
    observation_key,
    schema_version,
    contract_generation,
    scheduled_for,
    observed_at,
    source_event_at,
    scope_sha256,
    completeness,
    continuity,
    payload,
    payload_sha256,
    evidence_sha256,
    collector_instance,
    job_key,
    collection_job_kind,
    fence_epoch,
    projection_generation
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
    $11, $12, $13::jsonb, $14, $15, $16, $17, $18, $19, $20
)
RETURNING id
