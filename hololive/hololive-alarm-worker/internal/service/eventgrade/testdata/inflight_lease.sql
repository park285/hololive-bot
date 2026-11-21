WITH target AS (
    INSERT INTO event_grade_collection_targets(region,event_id,chapter_id,starts_at,ends_at,final_after)
    VALUES ($1,$2,$3,$4,$5,$6) RETURNING region,event_id,chapter_id
)
INSERT INTO event_grade_collection_leases(job_key,region,kind,event_id,chapter_id,target_generation,fence_epoch,owner_instance,claimed_at,lease_expires_at)
SELECT 'cutoff:test', region, 'cutoff', event_id, chapter_id, 1, 1, 'youtube-collector-c:test', statement_timestamp(), statement_timestamp() + interval '1 minute'
FROM target
