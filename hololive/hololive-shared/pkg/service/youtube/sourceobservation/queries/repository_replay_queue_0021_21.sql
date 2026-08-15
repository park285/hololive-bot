SELECT status,
       attempt_count,
       replay_count
FROM source_observation_queue
WHERE observation_id = $1
FOR UPDATE
