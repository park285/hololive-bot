INSERT INTO youtube_collection_targets (
    projection_generation, subject_key, observation_kind,
    priority, poll_interval_ms, enabled, valid_until
)
SELECT $1, input.subject_key, input.observation_kind,
       input.priority, input.poll_interval_ms, input.enabled, $7
FROM unnest($2::text[], $3::text[], $4::smallint[], $5::bigint[], $6::boolean[])
     AS input(subject_key, observation_kind, priority, poll_interval_ms, enabled)
