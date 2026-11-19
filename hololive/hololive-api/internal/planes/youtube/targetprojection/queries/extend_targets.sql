WITH current_targets AS MATERIALIZED (
    SELECT ctid, subject_key, observation_kind, not_before
    FROM youtube_collection_targets
    WHERE projection_generation = $1
), changed AS MATERIALIZED (
    SELECT target.ctid, input.not_before
    FROM current_targets AS target
    JOIN unnest($2::text[], $3::text[], $4::timestamptz[])
         AS input(subject_key, observation_kind, not_before)
      ON target.subject_key = input.subject_key
     AND target.observation_kind = input.observation_kind
    WHERE target.not_before IS DISTINCT FROM input.not_before
)
UPDATE youtube_collection_targets AS target
SET not_before = changed.not_before
FROM changed
WHERE target.ctid = changed.ctid
