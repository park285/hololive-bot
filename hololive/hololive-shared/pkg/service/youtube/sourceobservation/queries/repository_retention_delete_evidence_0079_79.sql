DELETE FROM source_observations AS observation
WHERE observation.id IN (
    SELECT candidate.id
    FROM source_observations AS candidate
    JOIN unnest($1::text[], $2::timestamptz[]) AS policy(observation_kind, cutoff)
      ON policy.observation_kind = candidate.observation_kind
    LEFT JOIN source_observation_queue AS queue
      ON queue.observation_id = candidate.id
    WHERE candidate.received_at < policy.cutoff
      AND queue.observation_id IS NULL
      AND NOT EXISTS (
          SELECT 1
          FROM source_observation_replay_requests AS replay
          WHERE replay.observation_id = candidate.id
            AND replay.status = 'PENDING'
      )
      AND NOT EXISTS (
          SELECT 1
          FROM youtube_live_reconciliation_heads AS head
          WHERE head.end_candidate_observation_id = candidate.id
      )
    ORDER BY candidate.received_at, candidate.id
    LIMIT $3
)
  AND NOT EXISTS (
      SELECT 1
      FROM source_observation_queue AS live_queue
      WHERE live_queue.observation_id = observation.id
  )
  AND NOT EXISTS (
      SELECT 1
      FROM source_observation_replay_requests AS live_replay
      WHERE live_replay.observation_id = observation.id
        AND live_replay.status = 'PENDING'
  )
  AND NOT EXISTS (
      SELECT 1
      FROM youtube_live_reconciliation_heads AS live_head
      WHERE live_head.end_candidate_observation_id = observation.id
  )
