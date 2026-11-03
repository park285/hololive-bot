WITH clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now_at
), valid_failure(error_code, failure_class) AS MATERIALIZED (
    VALUES
      ('observation_collision', 'DATA_CONTRACT')
), requested AS MATERIALIZED (
    SELECT vf.error_code,
           vf.failure_class,
           $8::text AS failure_detail
    FROM valid_failure AS vf
    WHERE vf.error_code = $6::text
      AND vf.failure_class = $7::text
      AND octet_length($8::text) BETWEEN 1 AND 2048
), updated AS (
    UPDATE youtube_collection_job_leases AS jobs
    SET slot_state = 'IDLE',
        owner_instance = NULL,
        lease_expires_at = NULL,
        retry_not_before = NULL,
        last_completed_at = clock.now_at,
        last_error_code = requested.error_code,
        last_failure_code = requested.error_code,
        last_failure_class = requested.failure_class,
        last_failure_detail = requested.failure_detail,
        last_failure_at = clock.now_at,
        next_due_at = jobs.scheduled_for + (jobs.poll_interval_ms * INTERVAL '1 millisecond'),
        updated_at = clock.now_at
    FROM clock, requested
    WHERE jobs.job_key = $1
      AND jobs.owner_instance = $2
      AND jobs.fence_epoch = $3
      AND jobs.projection_generation = $4
      AND jobs.scheduled_for = $5
      AND jobs.slot_state = 'ACTIVE'
      AND jobs.lease_expires_at > clock.now_at
    RETURNING jobs.job_key
)
SELECT job_key FROM updated;
