WITH extended AS (
    UPDATE source_observation_queue
    SET lease_expires_at = NOW() + ($3::bigint * INTERVAL '1 millisecond'),
        updated_at = NOW()
    WHERE observation_id = $1
      AND status = 'PROCESSING'
      AND lease_token = $2
      AND lease_expires_at > NOW()
      AND lease_expires_at < NOW() + ($3::bigint * INTERVAL '1 millisecond')
    RETURNING lease_expires_at
)
SELECT lease_expires_at
FROM extended
UNION ALL
SELECT lease_expires_at
FROM source_observation_queue
WHERE observation_id = $1
  AND status = 'PROCESSING'
  AND lease_token = $2
  AND lease_expires_at >= NOW() + ($3::bigint * INTERVAL '1 millisecond')
