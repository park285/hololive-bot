UPDATE source_observation_queue
SET lease_expires_at = GREATEST(
        lease_expires_at,
        NOW() + ($3::bigint * INTERVAL '1 millisecond')
    ),
    updated_at = NOW()
WHERE observation_id = $1
  AND status = 'PROCESSING'
  AND lease_token = $2
  AND lease_expires_at > NOW()
RETURNING lease_expires_at
