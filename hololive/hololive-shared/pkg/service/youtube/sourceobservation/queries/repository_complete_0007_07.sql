UPDATE source_observation_outbox
SET status = 'PROCESSED',
    lease_owner = NULL,
    lease_token = NULL,
    lease_expires_at = NULL,
    parity_status = $5,
    parity_detail = $6::jsonb,
    processed_at = NOW(),
    dead_lettered_at = NULL,
    last_error_code = NULL,
    last_error_detail = NULL,
    updated_at = NOW()
WHERE id = $1
  AND source_kind = $2
  AND status = 'PROCESSING'
  AND lease_token = $3
  AND generation = $4
RETURNING id
