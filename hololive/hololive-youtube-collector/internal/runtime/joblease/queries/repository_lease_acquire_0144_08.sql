-- 종료 처리 중인 소유자가 행을 잠그고 있으면 기다리지 않고 건너뛴다. 그 소유자가 끝내면 다음 주기에 다시 판정한다.
-- IDLE의 next_due_at은 직전 획득 때의 주기로 계산된 값이다. target 주기가 줄었으면 직전 slot에 현재 주기($4)를
-- 더한 시각을 실효 due이자 date_bin 기준점으로 써서, 긴 주기의 남은 대기 없이 현재 주기로 재개한다. 기준점이
-- 직전 slot보다 늦으므로 scheduled_for는 계속 단조 증가하고, 주기가 같거나 늘면 기존 next_due_at이 그대로 쓰인다.
WITH candidate AS (
    SELECT job_key
    FROM youtube_collection_job_leases
    WHERE job_key = $1
      AND (
          (slot_state = 'IDLE'
           AND LEAST(next_due_at, scheduled_for + $4::bigint * INTERVAL '1 millisecond') <= clock_timestamp())
          OR (slot_state = 'DEFERRED' AND retry_not_before <= clock_timestamp())
          OR (slot_state = 'ACTIVE' AND lease_expires_at <= clock_timestamp())
      )
      AND (owner_instance IS NULL OR lease_expires_at <= clock_timestamp())
    FOR UPDATE SKIP LOCKED
)
UPDATE youtube_collection_job_leases AS jobs
SET owner_instance = $2,
    fence_epoch = jobs.fence_epoch + 1,
    projection_generation = $3,
    poll_interval_ms = $4,
    scheduled_for = CASE
        WHEN jobs.slot_state = 'IDLE' THEN date_bin(
            $4::bigint * INTERVAL '1 millisecond',
            clock_timestamp(),
            LEAST(jobs.next_due_at, jobs.scheduled_for + $4::bigint * INTERVAL '1 millisecond')
        )
        ELSE jobs.scheduled_for
    END,
    slot_state = 'ACTIVE',
    retry_not_before = NULL,
    lease_expires_at = clock_timestamp() + ($5::bigint * INTERVAL '1 millisecond'),
    last_error_code = NULL,
    membership_kinds = $6::text[],
    membership_exact_subject = $7::boolean,
    membership_target_count = $8::integer,
    updated_at = clock_timestamp()
FROM candidate
WHERE jobs.job_key = candidate.job_key
RETURNING jobs.job_key,
          jobs.provider,
          jobs.job_class,
          jobs.collection_job_kind,
          jobs.subject_key,
          jobs.owner_instance,
          jobs.fence_epoch,
          jobs.projection_generation,
          jobs.scheduled_for
