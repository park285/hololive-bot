INSERT INTO source_observation_collisions (
    existing_observation_id,
    provider,
    observation_kind,
    subject_key,
    observation_key,
    schema_version,
    contract_generation,
    existing_evidence_sha256,
    attempted_evidence_sha256,
    attempted_payload_sha256,
    collector_instance,
    job_key,
    fence_epoch
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
