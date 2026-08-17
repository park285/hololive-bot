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
)
UPDATE youtube_collection_job_leases AS jobs
SET slot_state = 'DEFERRED',
    owner_instance = NULL,
    lease_expires_at = NULL,
    retry_not_before = LEAST(
        GREATEST($6, statement_timestamp() + ($10::bigint * INTERVAL '1 millisecond')),
        statement_timestamp() + ($11::bigint * INTERVAL '1 millisecond')
    ),
    last_error_code = requested.error_code,
    last_failure_code = requested.error_code,
    last_failure_class = requested.failure_class,
    last_failure_detail = $9,
    last_failure_at = clock_timestamp(),
    updated_at = clock_timestamp()
FROM requested
WHERE jobs.job_key = $1
  AND jobs.owner_instance = $2
  AND jobs.fence_epoch = $3
  AND jobs.projection_generation = $4
  AND jobs.scheduled_for = $5
  AND jobs.slot_state = 'ACTIVE'
  AND jobs.lease_expires_at > clock_timestamp()
RETURNING jobs.job_key
