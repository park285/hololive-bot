UPDATE source_observation_outbox
SET status = CASE
        WHEN attempt_count >= $7 THEN 'DEAD_LETTER'
        ELSE 'PENDING'
    END,
    available_at = CASE
        WHEN attempt_count >= $7 THEN available_at
        ELSE NOW() + ($4::bigint * INTERVAL '1 millisecond')
    END,
    lease_owner = NULL,
    lease_token = NULL,
    lease_expires_at = NULL,
    processed_at = NULL,
    dead_lettered_at = CASE
        WHEN attempt_count >= $7 THEN NOW()
        ELSE NULL
    END,
    last_error_code = CASE
        WHEN attempt_count >= $7 THEN 'attempts_exhausted'
        ELSE $5
    END,
    last_error_detail = NULLIF($6, ''),
    updated_at = NOW()
WHERE id = $1
  AND source_kind = $2
  AND status = 'PROCESSING'
  AND lease_token = $3
RETURNING status
