SELECT slot_state,
       retry_not_before
FROM youtube_collection_job_leases
WHERE job_key = $1
