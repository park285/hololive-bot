UPDATE youtube_collection_job_leases AS job
SET lease_expires_at = clock_timestamp() + ($6::bigint * INTERVAL '1 millisecond'),
    updated_at = clock_timestamp()
WHERE job.job_key = $1
  AND job.owner_instance = $2
  AND job.fence_epoch = $3
  AND job.projection_generation = $4
  AND job.scheduled_for = $5
  AND job.slot_state = 'ACTIVE'
  AND job.lease_expires_at > clock_timestamp()
  AND EXISTS (
      SELECT 1
      FROM youtube_collection_projection_generations AS generation
      WHERE generation.generation = job.projection_generation
        AND generation.status = 'CURRENT'
        AND generation.valid_until > clock_timestamp()
  )
RETURNING job.job_key
