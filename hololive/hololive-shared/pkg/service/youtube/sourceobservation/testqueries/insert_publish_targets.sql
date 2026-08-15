INSERT INTO youtube_collection_targets (
    projection_generation,
    subject_key,
    observation_kind,
    priority,
    poll_interval_ms,
    enabled,
    valid_until
)
SELECT $1,
       input.subject_key,
       input.observation_kind,
       50,
       60000,
       TRUE,
       clock_timestamp() + INTERVAL '1 hour'
FROM unnest($2::text[], $3::text[]) AS input(subject_key, observation_kind)
