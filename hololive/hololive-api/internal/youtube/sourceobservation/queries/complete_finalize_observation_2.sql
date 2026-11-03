INSERT INTO source_observation_consumer_offsets (
    consumer_name,
    observation_kind,
    last_processed_id,
    last_effective_at,
    last_processed_at
)
VALUES ($1, $2, $3, $4, NOW())
ON CONFLICT (consumer_name, observation_kind) DO UPDATE
SET last_effective_at = CASE
        WHEN EXCLUDED.last_processed_id >= source_observation_consumer_offsets.last_processed_id
            THEN EXCLUDED.last_effective_at
        ELSE source_observation_consumer_offsets.last_effective_at
    END,
    last_processed_id = GREATEST(
        source_observation_consumer_offsets.last_processed_id,
        EXCLUDED.last_processed_id
    ),
    last_processed_at = NOW(),
    updated_at = NOW()
