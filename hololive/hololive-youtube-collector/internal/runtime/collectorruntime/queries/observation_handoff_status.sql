WITH requested AS (
    SELECT observation_id, ordinal
    FROM unnest($1::bigint[]) WITH ORDINALITY AS ids(observation_id, ordinal)
)
SELECT requested.observation_id,
       queue.status
FROM requested
LEFT JOIN source_observation_queue AS queue
  ON queue.observation_id = requested.observation_id
ORDER BY requested.ordinal;
