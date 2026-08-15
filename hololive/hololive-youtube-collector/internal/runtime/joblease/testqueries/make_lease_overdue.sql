UPDATE youtube_collection_job_leases
SET next_due_at = clock_timestamp() - INTERVAL '10 minutes'
WHERE job_key = $1
