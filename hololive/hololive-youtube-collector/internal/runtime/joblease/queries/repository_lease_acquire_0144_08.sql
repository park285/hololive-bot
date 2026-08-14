UPDATE youtube_collection_job_leases
SET owner_instance = $2,
    fence_epoch = fence_epoch + 1,
    projection_generation = $3,
    poll_interval_ms = $4,
    scheduled_for = CASE
        WHEN slot_state = 'IDLE' THEN date_bin(
            $4::bigint * INTERVAL '1 millisecond',
            clock_timestamp(),
            next_due_at
        )
        ELSE scheduled_for
    END,
    slot_state = 'ACTIVE',
    retry_not_before = NULL,
    lease_expires_at = clock_timestamp() + ($5::bigint * INTERVAL '1 millisecond'),
    last_error_code = NULL,
    updated_at = clock_timestamp()
WHERE job_key = $1
  AND (
      (slot_state = 'IDLE' AND next_due_at <= clock_timestamp())
      OR (slot_state = 'DEFERRED' AND retry_not_before <= clock_timestamp())
      OR (slot_state = 'ACTIVE' AND lease_expires_at <= clock_timestamp())
  )
  AND (owner_instance IS NULL OR lease_expires_at <= clock_timestamp())
RETURNING job_key,
          collection_job_kind,
          owner_instance,
          fence_epoch,
          projection_generation,
          scheduled_for
