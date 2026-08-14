SELECT count(*)
FROM source_observation_replay_requests
WHERE observation_id = $1
  AND status = 'APPLIED'
