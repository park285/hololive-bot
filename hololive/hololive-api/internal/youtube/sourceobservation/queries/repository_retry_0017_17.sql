UPDATE source_observation_queue
SET status = CASE WHEN attempt_count >= $6 THEN 'DEAD_LETTER' ELSE 'PENDING' END,
    available_at = CASE
        WHEN attempt_count >= $6 THEN available_at
        ELSE clock_timestamp() + ($3::bigint * INTERVAL '1 millisecond')
    END,
    lease_owner = NULL,
    lease_token = NULL,
    lease_expires_at = NULL,
    processed_at = NULL,
    dead_lettered_at = CASE WHEN attempt_count >= $6 THEN clock_timestamp() ELSE NULL END,
    last_error_code = CASE WHEN attempt_count >= $6 THEN 'attempts_exhausted' ELSE $4 END,
    last_error_detail = NULLIF($5, ''),
    updated_at = clock_timestamp()
WHERE observation_id = $1
  AND status = 'PROCESSING'
  AND lease_token = $2
  AND lease_expires_at > clock_timestamp()
RETURNING status
