SELECT count(job_key)
FROM youtube_collection_job_leases
WHERE job_key = $1
  AND slot_state = 'ACTIVE'
