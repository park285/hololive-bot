WITH ready AS (
    SELECT observation.id, observation.received_at
    FROM source_observation_queue AS queue
    JOIN source_observations AS observation
      ON observation.id = queue.observation_id
    WHERE observation.observation_kind = ANY($1::text[])
      AND queue.attempt_count < $2
      AND (
          (queue.status = 'PENDING' AND queue.available_at <= clock_timestamp())
          OR
          (queue.status = 'PROCESSING' AND queue.lease_expires_at <= clock_timestamp())
      )
)
SELECT COUNT(id),
       COALESCE(GREATEST(EXTRACT(EPOCH FROM (clock_timestamp() - MIN(received_at))), 0), 0)
FROM ready
