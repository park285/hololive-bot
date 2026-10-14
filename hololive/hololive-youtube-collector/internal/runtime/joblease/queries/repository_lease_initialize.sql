-- 최초 INSERT는 유일 키 경쟁자의 rollback을 기다릴 수 있다. INSERT 입력 계산 시각이 아니라
-- 경쟁이 끝난 뒤의 DB 시계로 최초 slot과 TTL을 확정한다. 생성한 트랜잭션만 이 행을 잠근 채 호출한다.
UPDATE youtube_collection_job_leases
SET scheduled_for = date_bin(
        poll_interval_ms * INTERVAL '1 millisecond',
        clock_timestamp(),
        next_due_at
    ),
    lease_expires_at = clock_timestamp() + ($5::bigint * INTERVAL '1 millisecond'),
    updated_at = clock_timestamp()
WHERE job_key = $1
  AND owner_instance = $2
  AND fence_epoch = $3
  AND projection_generation = $4
RETURNING scheduled_for
