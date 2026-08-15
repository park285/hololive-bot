SELECT locked.generation
FROM (
    SELECT generation
    FROM youtube_collection_projection_generations
    WHERE status = 'CURRENT'
      AND valid_until > clock_timestamp()
) AS current_projection
CROSS JOIN LATERAL lock_youtube_collection_projection(current_projection.generation) AS locked
