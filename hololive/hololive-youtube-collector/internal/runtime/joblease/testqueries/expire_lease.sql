UPDATE youtube_collection_job_leases
SET lease_expires_at = clock_timestamp() - INTERVAL '1 second'
WHERE job_key = $1
