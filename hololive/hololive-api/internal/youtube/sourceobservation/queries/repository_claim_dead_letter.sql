UPDATE source_observation_queue AS queue
SET status = 'DEAD_LETTER',
    lease_owner = NULL,
    lease_token = NULL,
    lease_expires_at = NULL,
    processed_at = NULL,
    dead_lettered_at = NOW(),
    last_error_code = settled.error_code,
    last_error_detail = NULL,
    updated_at = NOW()
FROM unnest($1::bigint[], $2::text[]) AS settled(observation_id, error_code)
WHERE queue.observation_id = settled.observation_id
  AND (
      (queue.status = 'PENDING' AND queue.available_at <= NOW())
      OR
      (queue.status = 'PROCESSING' AND queue.lease_expires_at <= NOW())
  )
