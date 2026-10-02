SELECT id, observation_id
FROM source_observation_replay_requests
WHERE status = 'PENDING'
ORDER BY requested_at, id
LIMIT 1
FOR UPDATE SKIP LOCKED
