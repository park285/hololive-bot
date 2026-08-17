INSERT INTO youtube_collection_targets (
    projection_generation,
    subject_key,
    observation_kind,
    priority,
    poll_interval_ms,
    enabled,
    valid_until
) VALUES ($1, $2, $3, 50, $4, TRUE, clock_timestamp() + INTERVAL '1 hour')
