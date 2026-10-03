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
), pending_candidates AS MATERIALIZED (
    -- PENDING과 만료 PROCESSING을 가지별로 나눈다. OR 하나로 묶으면 backlog 전체를 정렬한 뒤 자르므로
    -- claim 비용이 PENDING backlog 크기에 비례한다.
    -- PENDING 가지는 idx_source_observation_queue_claim(available_at, observation_id) 순서로 LIMIT $2까지만 읽는다.
    -- 만료 PROCESSING 가지는 그 순서의 인덱스가 없어 lease_recovery 인덱스로 만료 행을 모두 읽고 정렬한 뒤 자른다.
    -- 만료 행은 lease를 잡고 finalize하지 못한 행뿐이라 PENDING backlog가 아니라 그런 claim 수(1회당 최대 $2행)에 비례한다.
    -- 두 가지의 상위 $2개 합집합에서 다시 상위 $2개를 고르므로 선택 결과는 단일 정렬과 같다.
    -- 한 claim은 최대 2×$2행을 잠그고, 선택되지 않은 최대 $2개 행의 FOR UPDATE 잠금도 claim 트랜잭션이 끝날 때
    -- (ClaimBatch 커밋, ProbeClaim 롤백)까지 유지된다. 그동안 다른 claimer는 SKIP LOCKED로 이 행을 건너뛴다.
    SELECT queue.observation_id, queue.available_at
    FROM source_observation_queue AS queue
    JOIN source_observations AS observation
      ON observation.id = queue.observation_id
    WHERE observation.observation_kind = ANY($1::text[])
      AND queue.status = 'PENDING'
      AND queue.available_at <= NOW()
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
), expired_candidates AS MATERIALIZED (
    SELECT queue.observation_id, queue.available_at
    FROM source_observation_queue AS queue
    JOIN source_observations AS observation
      ON observation.id = queue.observation_id
    WHERE observation.observation_kind = ANY($1::text[])
      AND queue.status = 'PROCESSING'
      AND queue.lease_expires_at <= NOW()
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
), candidates AS MATERIALIZED (
    SELECT branch.observation_id
    FROM (
        SELECT observation_id, available_at FROM pending_candidates
        UNION ALL
        SELECT observation_id, available_at FROM expired_candidates
    ) AS branch
    ORDER BY branch.available_at, branch.observation_id
    LIMIT $2
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
