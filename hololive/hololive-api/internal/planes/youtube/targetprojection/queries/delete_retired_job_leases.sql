WITH candidate AS (
    SELECT lease.job_key
    FROM youtube_collection_job_leases AS lease
    JOIN youtube_collection_projection_generations AS generation
      ON generation.generation = lease.projection_generation
    WHERE generation.status = 'RETIRED'
      AND generation.valid_until < $1
      AND (
          lease.slot_state <> 'ACTIVE'
          OR lease.lease_expires_at < clock_timestamp()
      )
    ORDER BY generation.generation, lease.job_key
    LIMIT $2
    FOR UPDATE OF lease SKIP LOCKED
)
DELETE FROM youtube_collection_job_leases AS lease
USING candidate
WHERE lease.job_key = candidate.job_key
