replay_epoch AS MATERIALIZED (
    -- singleton 상한은 빈 epoch 테이블의 과대 추정과 JIT 비용을 막는다.
    SELECT cutoff_received_at
    FROM source_observation_replay_epoch
    WHERE singleton
    LIMIT 1
), active_backlog AS MATERIALIZED (
    -- 활성 backlog 단일 pass는 미래·임대·시도 소진 목록 선두도 보존한다.
    SELECT queue.observation_id,
           queue.available_at,
           CASE WHEN queue.status = 'PROCESSING' THEN queue.lease_expires_at ELSE queue.available_at END AS eligible_at,
           queue.attempt_count,
           observation.observation_kind,
           observation.subject_key,
           observation.scheduled_for,
           (queue.status = 'PENDING' AND queue.available_at <= NOW())
           OR
           (queue.status = 'PROCESSING' AND queue.lease_expires_at <= NOW()) AS due
    FROM source_observation_queue AS queue
    JOIN source_observations AS observation ON observation.id = queue.observation_id
    WHERE (queue.status = 'PENDING' OR queue.status = 'PROCESSING')
      AND observation.observation_kind = ANY($1::text[])
      AND NOT EXISTS (
          SELECT 1
          FROM replay_epoch AS epoch
          WHERE observation.received_at < epoch.cutoff_received_at
      )
), list_heads AS (
    SELECT DISTINCT ON (active.observation_kind, active.subject_key)
           active.observation_id,
           active.available_at,
           active.eligible_at,
           active.due AND active.attempt_count < {{max_attempts}} AS claimable
    FROM active_backlog AS active
    WHERE active.observation_kind IN ('shorts_list', 'video_list')
    ORDER BY active.observation_kind, active.subject_key, active.scheduled_for, active.observation_id
), claim_order AS (
    SELECT active.observation_id, active.available_at, active.eligible_at
    FROM active_backlog AS active
    WHERE active.due
      AND active.attempt_count < {{max_attempts}}
      AND active.observation_kind NOT IN ('shorts_list', 'video_list')
    UNION ALL
    SELECT head.observation_id, head.available_at, head.eligible_at
    FROM list_heads AS head
    WHERE head.claimable
)
