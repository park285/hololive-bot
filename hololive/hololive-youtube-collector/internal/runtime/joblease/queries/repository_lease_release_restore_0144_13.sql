UPDATE youtube_collection_job_leases
SET last_failure_code = $2,
    last_failure_class = $3,
    last_failure_detail = $4,
    last_failure_at = $5
WHERE job_key = $1
RETURNING job_key
