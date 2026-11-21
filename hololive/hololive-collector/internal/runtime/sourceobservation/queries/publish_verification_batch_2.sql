SELECT provider,
       collection_job_kind,
       job_class,
       subject_key
FROM youtube_collection_job_leases
WHERE job_key = $1
  AND owner_instance = $2
  AND fence_epoch = $3
  AND projection_generation = $4
  AND scheduled_for = $5
  AND slot_state = 'ACTIVE'
  AND lease_expires_at > clock_timestamp()
FOR UPDATE
