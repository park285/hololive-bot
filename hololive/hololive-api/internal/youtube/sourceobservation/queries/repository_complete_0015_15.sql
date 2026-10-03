UPDATE source_observation_queue
SET status = 'PROCESSED',
    lease_owner = NULL,
    lease_token = NULL,
    lease_expires_at = NULL,
    processed_at = clock_timestamp(),
    dead_lettered_at = NULL,
    last_error_code = NULL,
    last_error_detail = NULL,
    updated_at = clock_timestamp()
WHERE observation_id = $1
  AND status = 'PROCESSING'
  AND lease_token = $2
  AND lease_expires_at > clock_timestamp()
RETURNING observation_id
