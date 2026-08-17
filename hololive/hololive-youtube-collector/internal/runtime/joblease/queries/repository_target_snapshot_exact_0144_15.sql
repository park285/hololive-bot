WITH requested(kind) AS (
    SELECT unnest($2::text[])
), current_projection AS (
    SELECT generation
    FROM youtube_collection_projection_generations
    WHERE generation = $1
      AND status = 'CURRENT'
      AND valid_until > statement_timestamp()
), matched AS (
    SELECT requested.kind,
           current_projection.generation IS NOT NULL AS projection_current,
           EXISTS (
               SELECT 1
               FROM youtube_collection_targets AS targets
               WHERE targets.projection_generation = current_projection.generation
                 AND targets.subject_key = $3
                 AND targets.observation_kind = requested.kind
                 AND targets.enabled = TRUE
                 AND targets.valid_until > statement_timestamp()
           ) AS enabled
    FROM requested
    LEFT JOIN current_projection ON TRUE
)
SELECT kind, projection_current, enabled
FROM matched
ORDER BY kind
