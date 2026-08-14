UPDATE youtube_collection_job_leases
SET slot_state = 'ACTIVE',
    owner_instance = $2,
    fence_epoch = $3,
    scheduled_for = $4,
    next_due_at = $4,
    lease_expires_at = NOW() + INTERVAL '1 hour'
WHERE job_key = $1
