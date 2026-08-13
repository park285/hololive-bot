INSERT INTO source_observation_outbox (
    source_kind,
    source_key,
    observation_key,
    schema_version,
    generation,
    observed_at,
    completeness,
    continuity,
    payload,
    payload_sha256
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10)
ON CONFLICT (source_kind, source_key, observation_key, schema_version) DO NOTHING
RETURNING id
