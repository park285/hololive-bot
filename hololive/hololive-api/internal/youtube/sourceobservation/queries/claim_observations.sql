WITH {{ready_relation}}
, replay_expired_candidates AS MATERIALIZED (
    SELECT queue.observation_id, queue.attempt_count
    FROM source_observation_queue AS queue
    JOIN source_observations AS observation
      ON observation.id = queue.observation_id
    JOIN replay_epoch AS epoch
      ON observation.received_at < epoch.cutoff_received_at
    WHERE observation.observation_kind_id = ANY(ARRAY(
        SELECT kind.id FROM source_observation_kinds AS kind WHERE kind.name = ANY($1::text[])
    ))
      AND (
          (queue.status = 'PENDING' AND queue.available_at <= NOW())
          OR
          (queue.status = 'PROCESSING' AND queue.lease_expires_at <= NOW())
      )
    ORDER BY queue.available_at, queue.observation_id
    LIMIT $2
    FOR UPDATE OF queue SKIP LOCKED
), exhausted_candidates AS MATERIALIZED (
    SELECT locked.observation_id, locked.attempt_count
    FROM (
        SELECT active.observation_id, active.available_at
        FROM active_backlog AS active
        WHERE active.due
          AND active.attempt_count >= $6
        ORDER BY active.available_at, active.observation_id
    ) AS ordered
    CROSS JOIN LATERAL (
        SELECT queue.observation_id, queue.attempt_count
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
), candidates AS MATERIALIZED (
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
       TRUE AS claimed,
       claimed.lease_token,
       kind.name AS observation_kind,
       observation.subject_key,
       claimed.attempt_count,
       FALSE AS replay_epoch_rejected
FROM claimed
JOIN source_observations AS observation
  ON observation.id = claimed.observation_id
JOIN source_observation_kinds AS kind
  ON kind.id = observation.observation_kind_id
UNION ALL
SELECT replay.observation_id, FALSE, NULL, NULL, NULL, replay.attempt_count, TRUE
FROM replay_expired_candidates AS replay
UNION ALL
SELECT exhausted.observation_id, FALSE, NULL, NULL, NULL, exhausted.attempt_count, FALSE
FROM exhausted_candidates AS exhausted
ORDER BY 1
