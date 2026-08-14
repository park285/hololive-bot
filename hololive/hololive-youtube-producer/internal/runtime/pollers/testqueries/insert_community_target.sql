INSERT INTO youtube_collection_targets (
    projection_generation,
    subject_key,
    observation_kind,
    priority,
    poll_interval_ms,
    enabled,
    valid_until
) VALUES ($1, $2, 'community_page', 50, 60000, TRUE, NOW() + INTERVAL '1 day')
