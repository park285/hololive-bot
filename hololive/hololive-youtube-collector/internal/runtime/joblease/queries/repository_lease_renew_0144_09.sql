-- 같은 트랜잭션이 lease 행을 잠그고 membership을 판정한 뒤에만 실행한다. 소유 증명은 그대로 다시 확인한다.
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
RETURNING job.job_key
