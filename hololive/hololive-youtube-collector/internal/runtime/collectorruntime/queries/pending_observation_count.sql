SELECT COUNT(observation_id)
FROM source_observation_queue
WHERE status IN ('PENDING', 'PROCESSING')
