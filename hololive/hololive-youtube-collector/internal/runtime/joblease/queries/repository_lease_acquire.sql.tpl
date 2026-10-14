-- 범위별 bundle SQL을 고정 자산에서 채운다. 거절 사유는 Go에서 순서대로 판정한다.
-- guard share 잠금 이후에만 실행하며 기존 행은 SKIP LOCKED로 선점한다.
-- 잠겨 있거나 아직 due가 아닌 기존 행은 missing이 아니므로 INSERT에 도달하지 않는다.
WITH bundle AS MATERIALIZED (
%s
),
admission AS MATERIALIZED (
    SELECT member_count::integer AS member_count
    FROM bundle
    WHERE current
      AND target_count > 0
      AND min_interval_ms > 0 AND min_interval_ms = max_interval_ms AND min_interval_ms = $7
      AND admissible_count > 0
      AND members_proven AND member_count >= target_count AND member_count BETWEEN 0 AND 2147483647
), existing AS MATERIALIZED (
    SELECT job_key
    FROM youtube_collection_job_leases
    WHERE job_key = $5
), candidate AS (
    SELECT jobs.job_key
    FROM youtube_collection_job_leases AS jobs
    CROSS JOIN admission
    WHERE jobs.job_key = $5
      AND (
          (jobs.slot_state = 'IDLE'
           AND LEAST(jobs.next_due_at, jobs.scheduled_for + $7::bigint * INTERVAL '1 millisecond') <= clock_timestamp())
          OR (jobs.slot_state = 'DEFERRED' AND jobs.retry_not_before <= clock_timestamp())
          OR (jobs.slot_state = 'ACTIVE' AND jobs.lease_expires_at <= clock_timestamp())
      )
      AND (jobs.owner_instance IS NULL OR jobs.lease_expires_at <= clock_timestamp())
    FOR UPDATE OF jobs SKIP LOCKED
), updated AS (
    UPDATE youtube_collection_job_leases AS jobs
    SET owner_instance = $6,
        fence_epoch = jobs.fence_epoch + 1,
        projection_generation = $1,
        poll_interval_ms = $7,
        -- 주기가 줄면 직전 slot + 현재 주기를 실효 due로 삼아 scheduled_for의 단조 증가를 보존한다.
        scheduled_for = CASE
            WHEN jobs.slot_state = 'IDLE' THEN date_bin(
                $7::bigint * INTERVAL '1 millisecond',
                clock_timestamp(),
                LEAST(jobs.next_due_at, jobs.scheduled_for + $7::bigint * INTERVAL '1 millisecond')
            )
            ELSE jobs.scheduled_for
        END,
        slot_state = 'ACTIVE',
        retry_not_before = NULL,
        lease_expires_at = clock_timestamp() + ($8::bigint * INTERVAL '1 millisecond'),
        last_error_code = NULL,
        membership_kinds = $3::text[],
        membership_exact_subject = $9::boolean,
        membership_target_count = admission.member_count,
        updated_at = clock_timestamp()
    FROM candidate, admission
    WHERE jobs.job_key = candidate.job_key
    RETURNING jobs.job_key, jobs.provider, jobs.job_class, jobs.collection_job_kind, jobs.subject_key,
              jobs.owner_instance, jobs.fence_epoch, jobs.projection_generation, jobs.scheduled_for
), missing AS MATERIALIZED (
    SELECT admission.member_count, clock_timestamp() AS scheduled_for
    FROM admission
    WHERE NOT EXISTS (SELECT 1 FROM existing)
), inserted AS (
    INSERT INTO youtube_collection_job_leases (
        job_key, provider, job_class, collection_job_kind, subject_key,
        owner_instance, fence_epoch, projection_generation, poll_interval_ms,
        scheduled_for, next_due_at, slot_state, lease_expires_at,
        membership_kinds, membership_exact_subject, membership_target_count, updated_at
    )
    SELECT $5, $10, $11, $12, $4, $6, 1, $1, $7,
           missing.scheduled_for, missing.scheduled_for, 'ACTIVE',
           clock_timestamp() + ($8::bigint * INTERVAL '1 millisecond'),
           $3::text[], $9::boolean, missing.member_count, clock_timestamp()
    FROM missing
    -- 최초 생성 경쟁의 패자는 이미 생성된 행을 덮어쓰거나 같은 문장에서 재선점하지 않는다.
    ON CONFLICT (job_key) DO NOTHING
    RETURNING job_key, provider, job_class, collection_job_kind, subject_key,
              owner_instance, fence_epoch, projection_generation, scheduled_for
), acquired AS (
    SELECT job_key, provider, job_class, collection_job_kind, subject_key,
           owner_instance, fence_epoch, projection_generation, scheduled_for, FALSE AS created FROM updated
    UNION ALL
    SELECT job_key, provider, job_class, collection_job_kind, subject_key,
           owner_instance, fence_epoch, projection_generation, scheduled_for, TRUE AS created FROM inserted
)
SELECT bundle.target_count, bundle.min_interval_ms, bundle.max_interval_ms,
       bundle.admissible_count, bundle.member_count, bundle.members_proven, bundle.current,
       acquired.job_key, acquired.provider, acquired.job_class, acquired.collection_job_kind, acquired.subject_key,
       acquired.owner_instance, acquired.fence_epoch, acquired.projection_generation, acquired.scheduled_for,
       COALESCE(acquired.created, FALSE)
FROM bundle
LEFT JOIN acquired ON TRUE
