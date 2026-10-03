WITH candidates AS (
    SELECT candidate.id
    FROM source_observation_replay_requests AS candidate
    WHERE candidate.status IN ('APPLIED', 'REJECTED')
      AND candidate.requested_at < $1
      AND NOT EXISTS (
          SELECT 1
          FROM source_observations AS observation
          WHERE observation.id = candidate.observation_id
      )
    ORDER BY candidate.requested_at, candidate.id
    LIMIT $2
    FOR UPDATE OF candidate SKIP LOCKED
)
DELETE FROM source_observation_replay_requests AS replay
USING candidates
WHERE replay.id = candidates.id
  AND replay.status IN ('APPLIED', 'REJECTED')
  AND NOT EXISTS (
      SELECT 1
      FROM source_observations AS observation
      WHERE observation.id = replay.observation_id
  )
