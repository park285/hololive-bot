SELECT observation.id
FROM source_observations AS observation
LEFT JOIN source_observation_queue AS queue
  ON queue.observation_id = observation.id
WHERE observation.observation_kind = $1
  AND observation.received_at < $2
  AND queue.observation_id IS NULL
  AND NOT EXISTS (
      SELECT 1
      FROM source_observation_replay_requests AS replay
      WHERE replay.observation_id = observation.id
        AND replay.status = 'PENDING'
  )
  AND NOT EXISTS (
      SELECT 1
      FROM youtube_live_reconciliation_heads AS head
      WHERE head.end_candidate_observation_id = observation.id
  )
ORDER BY observation.received_at, observation.id
LIMIT $3
