WITH exhausted_candidates AS MATERIALIZED (
    SELECT queue.observation_id
    FROM source_observation_queue AS queue
    JOIN source_observations AS observation
      ON observation.id = queue.observation_id
    WHERE observation.observation_kind = ANY($1::text[])
      AND queue.attempt_count >= $6
      AND (
          (queue.status = 'PENDING' AND queue.available_at <= NOW())
          OR
          (queue.status = 'PROCESSING' AND queue.lease_expires_at <= NOW())
      )
    ORDER BY queue.available_at, queue.observation_id
    LIMIT $2
    FOR UPDATE OF queue SKIP LOCKED
), exhausted AS (
    UPDATE source_observation_queue AS queue
    SET status = 'DEAD_LETTER',
        lease_owner = NULL,
        lease_token = NULL,
        lease_expires_at = NULL,
        processed_at = NULL,
        dead_lettered_at = NOW(),
        last_error_code = 'attempts_exhausted',
        last_error_detail = NULL,
        updated_at = NOW()
    FROM exhausted_candidates
    WHERE queue.observation_id = exhausted_candidates.observation_id
    RETURNING queue.observation_id
), candidates AS MATERIALIZED (
    SELECT queue.observation_id
    FROM source_observation_queue AS queue
    JOIN source_observations AS observation
      ON observation.id = queue.observation_id
    WHERE observation.observation_kind = ANY($1::text[])
      AND (
          (queue.status = 'PENDING' AND queue.available_at <= NOW())
          OR
          (queue.status = 'PROCESSING' AND queue.lease_expires_at <= NOW())
      )
      AND queue.attempt_count < $6
    ORDER BY queue.available_at, queue.observation_id
    LIMIT $2
    FOR UPDATE OF queue SKIP LOCKED
), claimed AS (
    UPDATE source_observation_queue AS queue
    SET status = 'PROCESSING',
        attempt_count = queue.attempt_count + 1,
        lease_owner = $3,
        lease_token = $4,
        lease_expires_at = NOW() + ($5::bigint * INTERVAL '1 millisecond'),
        processed_at = NULL,
        dead_lettered_at = NULL,
        updated_at = NOW()
    FROM candidates
    WHERE queue.observation_id = candidates.observation_id
      AND queue.attempt_count < $6
    RETURNING queue.observation_id,
              queue.attempt_count,
              queue.lease_owner,
              queue.lease_token,
              queue.lease_expires_at
)
SELECT observation.id,
       claimed.lease_token,
       observation.observation_kind,
       observation.subject_key
FROM claimed
JOIN source_observations AS observation
  ON observation.id = claimed.observation_id
ORDER BY observation.id
