WITH clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now_at
)
UPDATE youtube_collection_job_leases
SET scheduled_for = date_bin(
        poll_interval_ms * INTERVAL '1 millisecond',
        clock.now_at,
        next_due_at
    ),
    lease_expires_at = clock.now_at + ($5::bigint * INTERVAL '1 millisecond'),
    updated_at = clock.now_at
FROM clock
WHERE job_key = $1
  AND owner_instance = $2
  AND fence_epoch = $3
  AND projection_generation = $4
RETURNING scheduled_for
