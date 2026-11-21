UPDATE youtube_collection_job_leases
SET retry_not_before = clock_timestamp() - INTERVAL '1 millisecond'
WHERE job_key = $1
