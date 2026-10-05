WITH locked AS MATERIALIZED (
    SELECT job_key, owner_instance, fence_epoch, projection_generation,
           scheduled_for, slot_state, lease_expires_at
    FROM youtube_collection_job_leases
    WHERE job_key = $1
    FOR UPDATE
), eligible AS MATERIALIZED (
    SELECT locked.job_key, clock_timestamp() AS transitioned_at
    FROM locked
    WHERE locked.job_key = $1
      AND locked.owner_instance = $2
      AND locked.fence_epoch = $3
      AND locked.projection_generation = $4
      AND locked.scheduled_for = $5
      AND locked.slot_state = 'ACTIVE'
      AND locked.lease_expires_at > clock_timestamp()
)
UPDATE youtube_collection_job_leases AS jobs
SET slot_state = 'DEFERRED',
    owner_instance = NULL,
    lease_expires_at = NULL,
    retry_not_before = eligible.transitioned_at + ($6::bigint * INTERVAL '1 millisecond'),
    last_error_code = $7,
    updated_at = eligible.transitioned_at
FROM eligible
WHERE jobs.job_key = eligible.job_key
RETURNING jobs.job_key
