SELECT COUNT(queue.observation_id),
       COUNT(queue.observation_id) FILTER (WHERE queue.status = 'PROCESSING'),
       COALESCE(
           GREATEST(
               EXTRACT(EPOCH FROM (clock_timestamp() - MIN(observation.received_at))),
               0
           ),
           0
       ),
       -- DEAD_LETTER 부분 인덱스로 존재만 확인한다. replay나 retention이 지우면 degraded가 풀린다.
       EXISTS (
           SELECT 1
           FROM source_observation_queue AS dead_letter
           WHERE dead_letter.status = 'DEAD_LETTER'
       )
FROM source_observation_queue AS queue
JOIN source_observations AS observation
  ON observation.id = queue.observation_id
WHERE (queue.status = 'PENDING' OR queue.status = 'PROCESSING')
