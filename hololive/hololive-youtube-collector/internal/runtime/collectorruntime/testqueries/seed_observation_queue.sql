WITH sequence AS (
    SELECT ordinal
    FROM generate_series(1, $1::integer + $2::integer + $3::integer) AS ordinal
), observations AS (
    INSERT INTO source_observations (
        provider,
        observation_kind,
        subject_key,
        observation_key,
        schema_version,
        contract_generation,
        scheduled_for,
        observed_at,
        scope_sha256,
        completeness,
        continuity,
        payload,
        payload_sha256,
        evidence_sha256,
        collector_instance,
        job_key,
        collection_job_kind,
        fence_epoch,
        projection_generation
    )
    SELECT 'youtubejs',
           'community_page',
           $4::text || ':subject:' || sequence.ordinal::text,
           $4::text || ':observation:' || sequence.ordinal::text,
           1,
           1,
           clock_timestamp(),
           clock_timestamp(),
           repeat('a', 64),
           'COMPLETE',
           'CONTIGUOUS',
           '{}'::jsonb,
           repeat('b', 64),
           repeat('c', 64),
           'collector-test',
           $4::text || ':job:' || sequence.ordinal::text,
           'community_collect',
           1,
           1
    FROM sequence
    RETURNING id, observation_key
), queued AS (
    INSERT INTO source_observation_queue (
        observation_id,
        status,
        lease_owner,
        lease_token,
        lease_expires_at,
        processed_at
    )
    SELECT observations.id,
           CASE
               WHEN split_part(observations.observation_key, ':', 3)::integer <= $1::integer THEN 'PENDING'
               WHEN split_part(observations.observation_key, ':', 3)::integer <= $1::integer + $2::integer THEN 'PROCESSING'
               ELSE 'PROCESSED'
           END,
           CASE
               WHEN split_part(observations.observation_key, ':', 3)::integer > $1::integer
                AND split_part(observations.observation_key, ':', 3)::integer <= $1::integer + $2::integer
               THEN 'consumer-test'
           END,
           CASE
               WHEN split_part(observations.observation_key, ':', 3)::integer > $1::integer
                AND split_part(observations.observation_key, ':', 3)::integer <= $1::integer + $2::integer
               THEN repeat('d', 64)
           END,
           CASE
               WHEN split_part(observations.observation_key, ':', 3)::integer > $1::integer
                AND split_part(observations.observation_key, ':', 3)::integer <= $1::integer + $2::integer
               THEN clock_timestamp() + INTERVAL '1 minute'
           END,
           CASE
               WHEN split_part(observations.observation_key, ':', 3)::integer > $1::integer + $2::integer
               THEN clock_timestamp()
           END
    FROM observations
    RETURNING observation_id
)
SELECT COUNT(observation_id)::integer
FROM queued
