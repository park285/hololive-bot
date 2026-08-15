UPDATE youtube_collection_job_leases
SET slot_state = 'DEFERRED',
    owner_instance = NULL,
    lease_expires_at = NULL,
    retry_not_before = LEAST(
        GREATEST($6, statement_timestamp() + ($8::bigint * INTERVAL '1 millisecond')),
        statement_timestamp() + ($9::bigint * INTERVAL '1 millisecond')
    ),
    last_error_code = $7,
    updated_at = clock_timestamp()
WHERE job_key = $1
  AND owner_instance = $2
  AND fence_epoch = $3
  AND projection_generation = $4
  AND scheduled_for = $5
  AND slot_state = 'ACTIVE'
  AND lease_expires_at > clock_timestamp()
RETURNING job_key
