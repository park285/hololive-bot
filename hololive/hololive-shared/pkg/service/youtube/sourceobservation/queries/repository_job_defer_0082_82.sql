WITH clock AS MATERIALIZED (
    SELECT statement_timestamp() AS now_at,
           clock_timestamp() AS failure_at
), valid_failure(error_code, failure_class) AS MATERIALIZED (
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
           vf.failure_class,
           $8::text AS failure_detail
    FROM valid_failure AS vf
    WHERE vf.error_code = $6::text
      AND vf.failure_class = $7::text
      AND octet_length($8::text) BETWEEN 1 AND 2048
), candidate AS MATERIALIZED (
    SELECT CASE $9::text
             WHEN 'DELAY' THEN clock.now_at + ($10::bigint * INTERVAL '1 millisecond')
             WHEN 'AT' THEN $11::timestamptz
           END AS retry_at,
           clock.now_at,
           clock.failure_at,
           ($12::bigint * INTERVAL '1 millisecond') AS min_delay,
           ($13::bigint * INTERVAL '1 millisecond') AS max_delay
    FROM clock
), updated AS (
    UPDATE youtube_collection_job_leases AS jobs
    SET slot_state = 'DEFERRED',
        owner_instance = NULL,
        lease_expires_at = NULL,
        retry_not_before = LEAST(
            candidate.now_at + candidate.max_delay,
            GREATEST(candidate.retry_at, candidate.now_at + candidate.min_delay)
        ),
        last_error_code = requested.error_code,
        last_failure_code = requested.error_code,
        last_failure_class = requested.failure_class,
        last_failure_detail = requested.failure_detail,
        last_failure_at = candidate.failure_at,
        updated_at = candidate.failure_at
    FROM candidate, requested
    WHERE jobs.job_key = $1
      AND jobs.owner_instance = $2
      AND jobs.fence_epoch = $3
      AND jobs.projection_generation = $4
      AND jobs.scheduled_for = $5
      AND jobs.slot_state = 'ACTIVE'
      AND jobs.lease_expires_at > candidate.failure_at
      AND candidate.retry_at IS NOT NULL
      AND $12::bigint > 0
      AND $13::bigint >= $12::bigint
    RETURNING jobs.job_key
)
SELECT job_key FROM updated;
