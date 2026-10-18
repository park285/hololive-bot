WITH {{ready_relation}}, replay_expired_candidates AS MATERIALIZED (
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
    -- 시도를 소진한 due 행도 활성 pass에서 고른다. queue를 따로 읽으면 활성 backlog가 클 때 planner가 queue
    -- seq scan으로 보존 이력까지 읽는다. 잠금은 candidates와 같이 정렬된 후보마다 queue PK로 요청하고,
    -- 잠근 최신 버전에 조건을 다시 적용한다.
    SELECT locked.observation_id
    FROM (
        SELECT active.observation_id, active.available_at
        FROM active_backlog AS active
        WHERE active.due
          AND active.attempt_count >= $6
        ORDER BY active.available_at, active.observation_id
    ) AS ordered
    CROSS JOIN LATERAL (
        SELECT queue.observation_id
        FROM source_observation_queue AS queue
        WHERE queue.observation_id = ordered.observation_id
          AND queue.attempt_count >= $6
          AND (
              (queue.status = 'PENDING' AND queue.available_at <= NOW())
              OR
              (queue.status = 'PROCESSING' AND queue.lease_expires_at <= NOW())
          )
        FOR UPDATE SKIP LOCKED
    ) AS locked
    ORDER BY ordered.available_at, ordered.observation_id
    LIMIT $2
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
    -- 후보를 (available_at, observation_id) 순서로 정렬한 뒤 앞에서부터 한 행씩 queue PK로 잠근다.
    -- FOR UPDATE가 든 LATERAL 하위 질의는 평탄화되지 않으므로 잠금은 정렬된 후보마다 요청 시점에만 일어나고,
    -- LIMIT $2가 차면 더 잠그지 않는다. 따라서 이 CTE의 queue 방문은 $2 + (다른 claimer가 잠가 건너뛴 행)이고,
    -- 선택되지 않은 행을 잠그지 않는다. 다른 claimer가 snapshot 이후 바꾼 행은 SKIP LOCKED로 건너뛰거나
    -- 잠금 뒤 최신 버전으로 아래 조건을 다시 평가해 버린다.
    SELECT locked.observation_id
    FROM (
        SELECT claim_order.observation_id, claim_order.available_at
        FROM claim_order
        ORDER BY claim_order.available_at, claim_order.observation_id
    ) AS ordered
    CROSS JOIN LATERAL (
        SELECT queue.observation_id
        FROM source_observation_queue AS queue
        WHERE queue.observation_id = ordered.observation_id
          AND queue.attempt_count < $6
          AND (
              (queue.status = 'PENDING' AND queue.available_at <= NOW())
              OR
              (queue.status = 'PROCESSING' AND queue.lease_expires_at <= NOW())
          )
        FOR UPDATE SKIP LOCKED
    ) AS locked
    ORDER BY ordered.available_at, ordered.observation_id
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
       observation.subject_key,
       claimed.attempt_count
FROM claimed
JOIN source_observations AS observation
  ON observation.id = claimed.observation_id
ORDER BY observation.id
