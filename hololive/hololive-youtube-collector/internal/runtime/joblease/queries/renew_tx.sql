-- 같은 트랜잭션이 lease 행을 잠그고 membership을 판정한 뒤에만 실행한다. 소유 증명은 그대로 다시 확인한다.
-- 행을 앞 문장이 잠갔으므로 clock은 잠금 대기 이후 시각이며, 만료 판정과 연장 기준이 이 값 하나를 쓴다.
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
