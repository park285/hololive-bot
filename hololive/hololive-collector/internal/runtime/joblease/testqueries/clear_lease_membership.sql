UPDATE youtube_collection_job_leases
SET membership_kinds = '{}'::text[],
    membership_target_count = 0
WHERE job_key = $1
