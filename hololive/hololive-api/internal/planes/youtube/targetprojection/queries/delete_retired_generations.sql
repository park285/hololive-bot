WITH candidate AS (
    SELECT generation.generation
    FROM youtube_collection_projection_generations AS generation
    WHERE generation.status = 'RETIRED'
      AND generation.valid_until < $1
      AND NOT EXISTS (
          SELECT 1
          FROM youtube_collection_job_leases AS lease
          WHERE lease.projection_generation = generation.generation
      )
    ORDER BY generation.generation
    LIMIT $2
    FOR UPDATE OF generation SKIP LOCKED
)
DELETE FROM youtube_collection_projection_generations AS generation
USING candidate
WHERE generation.generation = candidate.generation
