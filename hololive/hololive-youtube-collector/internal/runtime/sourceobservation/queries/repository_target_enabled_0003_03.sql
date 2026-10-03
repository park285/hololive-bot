WITH requested AS (
    SELECT input.subject_key,
           input.observation_kind
    FROM unnest($2::text[], $3::text[]) AS input(subject_key, observation_kind)
)
SELECT NOT EXISTS (
    SELECT 1
    FROM requested
    WHERE NOT EXISTS (
        SELECT 1
        FROM youtube_collection_targets AS target
        WHERE target.projection_generation = $1
          AND target.subject_key = requested.subject_key
          AND target.observation_kind = requested.observation_kind
          AND target.enabled = TRUE
          AND target.valid_until > clock_timestamp()
    )
)
