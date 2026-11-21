WITH pending AS (
    SELECT observation_id
    FROM source_observation_queue
    WHERE status = 'PENDING'
    LIMIT $1 + 1
), processing AS (
    SELECT observation_id
    FROM source_observation_queue
    WHERE status = 'PROCESSING'
    LIMIT $1 + 1
), bounded AS (
    SELECT observation_id
    FROM pending
    UNION ALL
    SELECT observation_id
    FROM processing
    LIMIT $1 + 1
)
SELECT COALESCE(SUM(1), 0)::integer
FROM bounded;
