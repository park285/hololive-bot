WITH requested(kind) AS (
    SELECT unnest($2::text[])
), current_projection AS (
    SELECT generation
    FROM youtube_collection_projection_generations
    WHERE generation = $1
      AND status = 'CURRENT'
      AND valid_until > statement_timestamp()
), sentinels AS (
    SELECT requested.kind,
           current_projection.generation IS NOT NULL AS projection_current,
           NULL::text AS subject_key
    FROM requested
    LEFT JOIN current_projection ON TRUE
), bounded_targets AS (
    SELECT requested.kind,
           TRUE AS projection_current,
           targets.subject_key
    FROM requested
    JOIN current_projection ON TRUE
    JOIN youtube_collection_targets AS targets
      ON targets.projection_generation = current_projection.generation
     AND targets.observation_kind = requested.kind
     AND targets.enabled = TRUE
     AND targets.valid_until > statement_timestamp()
    ORDER BY requested.kind, targets.subject_key
    LIMIT $3 + 1
), rows AS (
    SELECT kind, projection_current, subject_key
    FROM sentinels
    UNION ALL
    SELECT kind, projection_current, subject_key
    FROM bounded_targets
)
SELECT kind, projection_current, subject_key
FROM rows
ORDER BY kind, subject_key NULLS FIRST
