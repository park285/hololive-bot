SELECT provider,
       job_class,
       collection_job_kind,
       subject_key
FROM youtube_collection_job_leases
WHERE job_key = $1
FOR UPDATE
