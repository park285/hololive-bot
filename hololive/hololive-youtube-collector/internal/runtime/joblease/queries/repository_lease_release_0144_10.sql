WITH requested AS MATERIALIZED (
    SELECT reason
    FROM (
        VALUES
            ('shutdown_release'),
            ('superseded_release'),
            ('renew_failed_release')
    ) AS allowed(reason)
    WHERE reason = $7::text
)
UPDATE youtube_collection_job_leases AS jobs
SET slot_state = CASE requested.reason
        WHEN 'superseded_release' THEN 'IDLE'
        ELSE 'DEFERRED'
    END,
    owner_instance = NULL,
    lease_expires_at = NULL,
    retry_not_before = CASE requested.reason
        WHEN 'superseded_release' THEN NULL
        ELSE clock_timestamp() + ($6::bigint * INTERVAL '1 millisecond')
    END,
    last_error_code = requested.reason,
    next_due_at = CASE requested.reason
        WHEN 'superseded_release' THEN LEAST(jobs.next_due_at, clock_timestamp())
        ELSE jobs.next_due_at
    END,
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
