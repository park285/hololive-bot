UPDATE source_observation_queue
SET status = 'DEAD_LETTER',
    lease_owner = NULL,
    lease_token = NULL,
    lease_expires_at = NULL,
    processed_at = NULL,
    dead_lettered_at = clock_timestamp(),
    last_error_code = $3,
    last_error_detail = NULLIF($4, ''),
    updated_at = clock_timestamp()
WHERE observation_id = $1
  AND status = 'PROCESSING'
  AND lease_token = $2
  AND lease_expires_at > clock_timestamp()
RETURNING observation_id
