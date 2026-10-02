UPDATE youtube_collection_job_leases
SET slot_state = 'ACTIVE', owner_instance = $2, lease_expires_at = NOW() + INTERVAL '1 hour',
        retry_not_before = NULL, last_error_code = NULL
WHERE job_key = $1
