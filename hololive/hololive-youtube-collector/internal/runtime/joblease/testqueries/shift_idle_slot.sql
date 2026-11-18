UPDATE youtube_collection_job_leases
SET scheduled_for = clock_timestamp() - ($2::bigint * INTERVAL '1 millisecond'),
    next_due_at = clock_timestamp() + ($3::bigint * INTERVAL '1 millisecond')
WHERE job_key = $1
  AND slot_state = 'IDLE'
RETURNING scheduled_for
