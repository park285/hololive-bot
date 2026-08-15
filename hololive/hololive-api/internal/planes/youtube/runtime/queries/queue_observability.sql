SELECT COUNT(queue.observation_id),
       COUNT(queue.observation_id) FILTER (WHERE queue.status = 'PROCESSING'),
       COALESCE(
           GREATEST(
               EXTRACT(EPOCH FROM (clock_timestamp() - MIN(observation.received_at))),
               0
           ),
           0
       )
FROM source_observation_queue AS queue
JOIN source_observations AS observation
  ON observation.id = queue.observation_id
WHERE queue.status IN ('PENDING', 'PROCESSING')
