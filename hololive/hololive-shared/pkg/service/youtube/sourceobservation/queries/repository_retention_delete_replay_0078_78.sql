DELETE FROM source_observation_replay_requests
WHERE id IN (
    SELECT candidate.id
    FROM source_observation_replay_requests AS candidate
    WHERE candidate.status IN ('APPLIED', 'REJECTED')
      AND candidate.requested_at < $1
    ORDER BY candidate.requested_at, candidate.id
    LIMIT $2
)
