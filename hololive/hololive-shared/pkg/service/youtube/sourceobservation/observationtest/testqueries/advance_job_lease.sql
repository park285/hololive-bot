UPDATE youtube_collection_job_leases
SET slot_state = 'ACTIVE', owner_instance = $2, lease_expires_at = NOW() + INTERVAL '1 hour',
    retry_not_before = NULL, fence_epoch = $3, scheduled_for = $4, next_due_at = $4
WHERE job_key = $1
