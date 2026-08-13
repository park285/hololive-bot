INSERT INTO source_collection_checkpoints (
    source_kind,
    source_key,
    generation,
    observation_key,
    payload_sha256,
    completeness,
    continuity,
    collected_at,
    last_success_at,
    collection_latency_ms,
    last_error_code,
    last_error_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8, $9, NULL, NULL)
ON CONFLICT (source_kind, source_key) DO UPDATE
SET generation = EXCLUDED.generation,
    observation_key = EXCLUDED.observation_key,
    payload_sha256 = EXCLUDED.payload_sha256,
    completeness = EXCLUDED.completeness,
    continuity = EXCLUDED.continuity,
    collected_at = EXCLUDED.collected_at,
    last_success_at = EXCLUDED.last_success_at,
    collection_latency_ms = EXCLUDED.collection_latency_ms,
    last_error_code = NULL,
    last_error_at = NULL,
    updated_at = NOW()
