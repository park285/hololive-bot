INSERT INTO youtube_collection_targets (
    projection_generation,
    subject_key,
    observation_kind,
    priority,
    poll_interval_ms,
    enabled,
    valid_until,
    member_since_generation,
    not_before
)
SELECT $2,
       subject_key,
       observation_kind,
       priority,
       poll_interval_ms,
       enabled,
       clock_timestamp() + INTERVAL '1 hour',
       member_since_generation,
       not_before
FROM youtube_collection_targets
WHERE projection_generation = $1
  AND (subject_key, observation_kind) NOT IN (
      SELECT dropped.subject_key, dropped.observation_kind
      FROM unnest($3::text[], $4::text[]) AS dropped(subject_key, observation_kind)
  )
