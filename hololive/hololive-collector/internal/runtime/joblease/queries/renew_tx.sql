WITH clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now_at
)
UPDATE youtube_collection_job_leases AS job
SET lease_expires_at = clock.now_at + ($6::bigint * INTERVAL '1 millisecond'),
    updated_at = clock.now_at
FROM clock
WHERE job.job_key = $1
  AND job.owner_instance = $2
  AND job.fence_epoch = $3
  AND job.projection_generation = $4
  AND job.scheduled_for = $5
  AND job.slot_state = 'ACTIVE'
  AND job.lease_expires_at > clock.now_at
RETURNING job.job_key
