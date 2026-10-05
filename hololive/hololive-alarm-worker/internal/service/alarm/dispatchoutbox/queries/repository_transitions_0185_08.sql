UPDATE alarm_dispatch_deliveries d
SET status = 'retry', next_attempt_at = input.next_attempt_at,
    locked_by = NULL, locked_at = NULL, lock_expires_at = NULL,
    last_error = input.error, last_error_code = input.error_code, updated_at = NOW()
FROM jsonb_to_recordset($1::jsonb) AS input(
    id BIGINT, attempt_count INT, next_attempt_at TIMESTAMPTZ, error TEXT, error_code TEXT)
WHERE d.id = input.id
  AND input.attempt_count = d.attempt_count
  AND d.status IN ('leased', 'sending')
  AND d.locked_by = $2
RETURNING d.id
