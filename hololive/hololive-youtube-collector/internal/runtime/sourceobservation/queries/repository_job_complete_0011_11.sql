UPDATE youtube_collection_job_leases
SET slot_state = 'IDLE',
    owner_instance = NULL,
    lease_expires_at = NULL,
    retry_not_before = NULL,
    last_completed_at = clock_timestamp(),
    last_error_code = NULL,
    next_due_at = scheduled_for + (poll_interval_ms * INTERVAL '1 millisecond'),
    updated_at = clock_timestamp()
WHERE job_key = $1
  AND owner_instance = $2
  AND fence_epoch = $3
  AND projection_generation = $4
  AND scheduled_for = $5
  AND slot_state = 'ACTIVE'
  AND lease_expires_at > clock_timestamp()
RETURNING job_key
