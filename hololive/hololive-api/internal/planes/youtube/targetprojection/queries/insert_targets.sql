INSERT INTO youtube_collection_targets (
    projection_generation, subject_key, observation_kind,
    priority, poll_interval_ms, enabled,
    member_since_generation, created_at, not_before
)
SELECT $1, input.subject_key, input.observation_kind,
       input.priority, input.poll_interval_ms, input.enabled,
       COALESCE(previous.member_since_generation, $1),
       COALESCE(CASE WHEN previous.member_since_generation IS NOT NULL THEN previous.created_at END, now()),
       input.not_before
FROM unnest($2::text[], $3::text[], $4::smallint[], $5::bigint[], $6::boolean[], $8::timestamptz[])
     AS input(subject_key, observation_kind, priority, poll_interval_ms, enabled, not_before)
LEFT JOIN youtube_collection_targets AS previous
  ON previous.projection_generation = $7::bigint
 AND previous.subject_key = input.subject_key
 AND previous.observation_kind = input.observation_kind
 AND previous.priority = input.priority
 AND previous.poll_interval_ms = input.poll_interval_ms
 AND previous.enabled = input.enabled
