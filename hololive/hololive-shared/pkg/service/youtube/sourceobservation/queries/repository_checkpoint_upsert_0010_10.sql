INSERT INTO source_collection_checkpoints (
    provider,
    observation_kind,
    subject_key,
    scope_sha256,
    contract_generation,
    last_observation_key,
    last_evidence_sha256,
    last_scheduled_for,
    last_success_at,
    collection_latency_ms,
    continuity,
    cursor,
    last_error_code,
    last_error_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), $9, $10, $11::jsonb, NULL, NULL)
ON CONFLICT (provider, observation_kind, subject_key, scope_sha256) DO UPDATE
SET contract_generation = EXCLUDED.contract_generation,
    last_observation_key = EXCLUDED.last_observation_key,
    last_evidence_sha256 = EXCLUDED.last_evidence_sha256,
    last_scheduled_for = EXCLUDED.last_scheduled_for,
    last_success_at = EXCLUDED.last_success_at,
    collection_latency_ms = EXCLUDED.collection_latency_ms,
    continuity = EXCLUDED.continuity,
    cursor = EXCLUDED.cursor,
    last_error_code = NULL,
    last_error_at = NULL,
    updated_at = NOW()
WHERE (
    source_collection_checkpoints.contract_generation,
    source_collection_checkpoints.last_observation_key,
    source_collection_checkpoints.last_evidence_sha256,
    source_collection_checkpoints.last_scheduled_for,
    source_collection_checkpoints.collection_latency_ms,
    source_collection_checkpoints.continuity,
    source_collection_checkpoints.cursor,
    source_collection_checkpoints.last_error_code,
    source_collection_checkpoints.last_error_at
) IS DISTINCT FROM (
    EXCLUDED.contract_generation,
    EXCLUDED.last_observation_key,
    EXCLUDED.last_evidence_sha256,
    EXCLUDED.last_scheduled_for,
    EXCLUDED.collection_latency_ms,
    EXCLUDED.continuity,
    EXCLUDED.cursor,
    NULL::text,
    NULL::timestamptz
)
