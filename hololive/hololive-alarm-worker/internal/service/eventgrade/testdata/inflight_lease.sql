INSERT INTO event_grade_collection_leases(job_key,region,kind,event_id,chapter_id,fence_epoch,owner_instance,claimed_at,lease_expires_at)
VALUES ('cutoff:test', $1, 'cutoff', $2, $3, 1, 'collector-c:test', statement_timestamp(), statement_timestamp() + interval '1 minute')
