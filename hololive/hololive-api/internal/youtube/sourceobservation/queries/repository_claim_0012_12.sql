WITH replay_epoch AS MATERIALIZED (
    -- singleton boolean PRIMARY KEY + CHECK (singleton)이라 행은 최대 1개다. LIMIT 1은 결과를 바꾸지 않고 그 상한을
    -- planner에 알린다. 없으면 분석 전 빈 테이블 추정(400행)이 active_backlog anti join·replay 만료 join에 곱해져
    -- 총 비용이 64만~193만으로 부풀고(실제 0행), JIT inline·optimize 임계를 넘어 claim 1회 JIT만 0.5~0.8초가 들었다.
    SELECT cutoff_received_at
    FROM source_observation_replay_epoch
    WHERE singleton
    LIMIT 1
), active_backlog AS MATERIALIZED (
    -- 활성 queue(PENDING·PROCESSING) 행을 한 번만 읽어 claim 판정 재료를 모은다.
    -- 같은 채널·종류 목록의 선두 판정에는 차단된 후속 목록까지 포함한 활성 행 전체가 필요하므로 이 단일 pass는
    -- 활성 backlog에 선형이다. 보존 이력(PROCESSED·DEAD_LETTER)은 status 조건으로, 다른 kind는 kind 조건으로 버린다.
    SELECT queue.observation_id,
           queue.available_at,
           observation.observation_kind,
           observation.subject_key,
           observation.scheduled_for,
           queue.attempt_count < $6 AND (
               (queue.status = 'PENDING' AND queue.available_at <= NOW())
               OR
               (queue.status = 'PROCESSING' AND queue.lease_expires_at <= NOW())
           ) AS claimable
    FROM source_observation_queue AS queue
    JOIN source_observations AS observation ON observation.id = queue.observation_id
    WHERE (queue.status = 'PENDING' OR queue.status = 'PROCESSING')
      AND observation.observation_kind = ANY($1::text[])
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
), list_heads AS (
    -- 같은 채널·종류의 후속 목록이 앞선 목록을 추월하여 신규 영상을 기준 목록으로 삼지 못하게 합니다.
    -- 채널·종류마다 (scheduled_for, id)가 가장 앞선 활성 목록만 선두이며, 선두가 아직 claim할 수 없으면
    -- (lease 유지 중, available_at 미도래, attempts 소진) 그 채널·종류의 후속 목록은 이번 claim에서 제외된다.
    SELECT DISTINCT ON (active.observation_kind, active.subject_key)
           active.observation_id,
           active.available_at,
           active.claimable
    FROM active_backlog AS active
    WHERE active.observation_kind IN ('shorts_list', 'video_list')
    ORDER BY active.observation_kind, active.subject_key, active.scheduled_for, active.observation_id
), claim_order AS (
    -- 후보는 claim 가능한 비목록 행과 claim 가능한 목록 선두뿐이다. queue 순서로 걸으며 차단된 후속 목록을
    -- 건너뛰면 선두 하나를 찾는 비용이 그 채널의 backlog 길이에 비례하므로, 활성 pass에서 후보 집합을 먼저 만든다.
    SELECT active.observation_id, active.available_at
    FROM active_backlog AS active
    WHERE active.claimable
      AND active.observation_kind NOT IN ('shorts_list', 'video_list')
    UNION ALL
    SELECT head.observation_id, head.available_at
    FROM list_heads AS head
    WHERE head.claimable
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
       observation.subject_key
FROM claimed
JOIN source_observations AS observation
  ON observation.id = claimed.observation_id
ORDER BY observation.id
