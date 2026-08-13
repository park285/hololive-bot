UPDATE source_observation_outbox
SET status = 'DEAD_LETTER',
    lease_owner = NULL,
    lease_token = NULL,
    lease_expires_at = NULL,
    processed_at = NULL,
    dead_lettered_at = NOW(),
    last_error_code = $4,
    last_error_detail = NULLIF($5, ''),
    updated_at = NOW()
WHERE id = $1
  AND source_kind = $2
  AND status = 'PROCESSING'
  AND lease_token = $3
RETURNING id
