SELECT queue.status
FROM source_observation_queue AS queue
JOIN source_observations AS observation
  ON observation.id = queue.observation_id
WHERE observation.subject_key = $1
ORDER BY observation.id DESC
LIMIT 1
