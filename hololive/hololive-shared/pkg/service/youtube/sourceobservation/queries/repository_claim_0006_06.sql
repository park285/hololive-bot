WITH quarantined AS (
    UPDATE source_observation_outbox AS observation
    SET status = 'DEAD_LETTER',
        lease_owner = NULL,
        lease_token = NULL,
        lease_expires_at = NULL,
        processed_at = NULL,
        dead_lettered_at = NOW(),
        last_error_code = CASE
            WHEN observation.generation <> $7 THEN 'stale_generation'
            ELSE 'attempts_exhausted'
        END,
        last_error_detail = NULL,
        updated_at = NOW()
    WHERE observation.source_kind = $1
      AND (
          (observation.status = 'PENDING' AND observation.available_at <= NOW())
          OR
          (observation.status = 'PROCESSING' AND observation.lease_expires_at <= NOW())
      )
      AND (observation.generation <> $7 OR observation.attempt_count >= $6)
    RETURNING observation.id
), candidates AS (
    SELECT observation.id
    FROM source_observation_outbox AS observation
    WHERE observation.source_kind = $1
      AND observation.generation = $7
      AND observation.attempt_count < $6
      AND (
          (observation.status = 'PENDING' AND observation.available_at <= NOW())
          OR
          (observation.status = 'PROCESSING' AND observation.lease_expires_at <= NOW())
      )
    ORDER BY observation.id
    LIMIT $2
    FOR UPDATE SKIP LOCKED
), claimed AS (
    UPDATE source_observation_outbox AS observation
    SET status = 'PROCESSING',
        attempt_count = observation.attempt_count + 1,
        lease_owner = $3,
        lease_token = $4,
        lease_expires_at = NOW() + ($5::bigint * INTERVAL '1 millisecond'),
        updated_at = NOW()
    FROM candidates
    WHERE observation.id = candidates.id
    RETURNING observation.id,
              observation.source_kind,
              observation.source_key,
              observation.observation_key,
              observation.schema_version,
              observation.generation,
              observation.observed_at,
              observation.completeness,
              observation.continuity,
              observation.payload,
              observation.payload_sha256,
              observation.attempt_count,
              observation.lease_owner,
              observation.lease_token,
              observation.lease_expires_at
)
SELECT id,
       source_kind,
       source_key,
       observation_key,
       schema_version,
       generation,
       observed_at,
       completeness,
       continuity,
       payload,
       payload_sha256,
       attempt_count,
       lease_owner,
       lease_token,
       lease_expires_at
FROM claimed
ORDER BY id
