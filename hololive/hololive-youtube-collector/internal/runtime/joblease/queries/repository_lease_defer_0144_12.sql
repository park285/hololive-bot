WITH valid_failure(error_code, failure_class) AS MATERIALIZED (
    VALUES
      ('collection_failed', 'TRANSIENT'),
      ('collection_failed', 'PROTOCOL'),
      ('collection_timeout', 'TIMEOUT'),
      ('collection_canceled', 'CANCELED'),
      ('parser_drift', 'DATA_CONTRACT'),
      ('cooldown', 'COOLDOWN'),
      ('configuration_error', 'CONFIGURATION'),
      ('response_too_large', 'RESOURCE_LIMIT'),
      ('helper_busy', 'TRANSIENT'),
      ('helper_protocol_mismatch', 'PROTOCOL'),
      ('collection_internal_invariant', 'INTERNAL'),
      ('target_roster_too_large', 'RESOURCE_LIMIT'),
      ('publish_rejected', 'TRANSIENT'),
      ('publish_rejected', 'PROTOCOL'),
      ('publish_rejected', 'INTERNAL')
), requested AS MATERIALIZED (
    SELECT vf.error_code,
           vf.failure_class
    FROM valid_failure AS vf
    WHERE vf.error_code = $7::text
      AND vf.failure_class = $8::text
      AND octet_length($9::text) BETWEEN 1 AND 2048
      AND ($12::boolean = FALSE OR (vf.error_code = 'cooldown' AND vf.failure_class = 'COOLDOWN'))
), locked AS MATERIALIZED (
    SELECT jobs.job_key, jobs.owner_instance, jobs.fence_epoch, jobs.projection_generation,
           jobs.scheduled_for, jobs.slot_state, jobs.lease_expires_at
    FROM youtube_collection_job_leases AS jobs CROSS JOIN requested
    WHERE jobs.job_key = $1
    FOR UPDATE OF jobs
), eligible AS MATERIALIZED (
    -- 잠금 대기 후 소유권과 만료를 확인하고 재시도·진단 시각에 같은 DB 시각을 쓴다.
    SELECT locked.job_key, requested.error_code, requested.failure_class,
           clock_timestamp() AS transitioned_at
    FROM locked CROSS JOIN requested
    WHERE locked.job_key = $1
      AND locked.owner_instance = $2
      AND locked.fence_epoch = $3
      AND locked.projection_generation = $4
      AND locked.scheduled_for = $5
      AND locked.slot_state = 'ACTIVE'
      AND locked.lease_expires_at > clock_timestamp()
      AND $6::timestamptz IS NOT NULL
      AND isfinite($6::timestamptz)
      AND $10::bigint > 0
      AND $11::bigint BETWEEN $10::bigint AND 3600000
)
UPDATE youtube_collection_job_leases AS jobs
SET slot_state = 'DEFERRED',
    owner_instance = NULL,
    lease_expires_at = NULL,
    retry_not_before = CASE WHEN $12::boolean THEN
        GREATEST($6::timestamptz, eligible.transitioned_at + ($10::bigint * INTERVAL '1 millisecond'))
    ELSE LEAST(
        GREATEST($6, eligible.transitioned_at + ($10::bigint * INTERVAL '1 millisecond')),
        eligible.transitioned_at + ($11::bigint * INTERVAL '1 millisecond')
    ) END,
    last_error_code = eligible.error_code,
    last_failure_code = eligible.error_code,
    last_failure_class = eligible.failure_class,
    last_failure_detail = $9,
    last_failure_at = eligible.transitioned_at,
    updated_at = eligible.transitioned_at
FROM eligible
WHERE jobs.job_key = eligible.job_key
RETURNING jobs.job_key
