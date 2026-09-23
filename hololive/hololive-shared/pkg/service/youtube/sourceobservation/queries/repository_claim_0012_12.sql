WITH replay_epoch AS MATERIALIZED (
    SELECT cutoff_received_at
    FROM source_observation_replay_epoch
    WHERE singleton
), active_shorts AS MATERIALIZED (
    -- 채널의 보존 이력을 후보마다 훑지 않도록 활성 queue를 partial index로 먼저 좁힌다.
    SELECT observation.id, observation.subject_key, observation.scheduled_for
    FROM source_observation_queue AS queue
    JOIN source_observations AS observation ON observation.id = queue.observation_id
    WHERE (queue.status = 'PENDING' OR queue.status = 'PROCESSING')
      AND observation.observation_kind = 'shorts_list'
      AND NOT EXISTS (
          SELECT 1
          FROM replay_epoch AS epoch
          WHERE observation.received_at < epoch.cutoff_received_at
      )
), replay_expired_candidates AS MATERIALIZED (
    SELECT queue.observation_id
    FROM source_observation_queue AS queue
    JOIN source_observations AS observation
      ON observation.id = queue.observation_id
    JOIN replay_epoch AS epoch
      ON observation.received_at < epoch.cutoff_received_at
    WHERE observation.observation_kind = ANY($1::text[])
      AND (
          (queue.status = 'PENDING' AND queue.available_at <= NOW())
          OR
          (queue.status = 'PROCESSING' AND queue.lease_expires_at <= NOW())
      )
    ORDER BY queue.available_at, queue.observation_id
    LIMIT $2
    FOR UPDATE OF queue SKIP LOCKED
), replay_expired AS (
    UPDATE source_observation_queue AS queue
    SET status = 'DEAD_LETTER',
        lease_owner = NULL,
        lease_token = NULL,
        lease_expires_at = NULL,
        processed_at = NULL,
        dead_lettered_at = NOW(),
        last_error_code = 'replay_epoch_expired',
        last_error_detail = NULL,
        updated_at = NOW()
    FROM replay_expired_candidates
    WHERE queue.observation_id = replay_expired_candidates.observation_id
    RETURNING queue.observation_id
), exhausted_candidates AS MATERIALIZED (
    SELECT queue.observation_id
    FROM source_observation_queue AS queue
    JOIN source_observations AS observation
      ON observation.id = queue.observation_id
    WHERE observation.observation_kind = ANY($1::text[])
      AND queue.attempt_count >= $6
      AND NOT EXISTS (
          SELECT 1
          FROM replay_epoch AS epoch
          WHERE observation.received_at < epoch.cutoff_received_at
      )
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
      AND NOT EXISTS (
          SELECT 1
          FROM replay_epoch AS epoch
          WHERE observation.received_at < epoch.cutoff_received_at
      )
      AND (
          observation.observation_kind <> 'shorts_list'
          OR NOT EXISTS (
              -- 같은 채널의 후속 목록이 최초 목록을 추월하여 신규 쇼츠를 baseline으로 삼지 못하게 합니다.
              SELECT 1
              FROM active_shorts AS predecessor
              WHERE predecessor.subject_key = observation.subject_key
                AND (predecessor.scheduled_for, predecessor.id) < (observation.scheduled_for, observation.id)
          )
      )
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
