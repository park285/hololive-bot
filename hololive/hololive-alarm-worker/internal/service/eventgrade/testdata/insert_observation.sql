WITH bumped AS (
    UPDATE event_grade_regions SET published_seq = published_seq + 1 WHERE region = $1 RETURNING published_seq
)
INSERT INTO event_grade_observations(region,seq,schema_version,kind,job_key,scheduled_for,owner_instance,fence_epoch,target_generation,started_at,observed_at,published_at,payload)
SELECT $1, published_seq, 1, $2, 'test:' || published_seq, $3, 'youtube-collector-c:test', 1, CASE WHEN $2 = 'cutoff' THEN 1 END, $3, $4, statement_timestamp(), $5::jsonb
FROM bumped
RETURNING seq
