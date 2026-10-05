UPDATE alarm_dispatch_deliveries d
SET status = input.target_status, attempt_count = input.attempt_count,
    next_attempt_at = COALESCE(input.next_attempt_at, d.next_attempt_at),
    dlq_at = CASE WHEN input.mark_dlq THEN NOW() ELSE d.dlq_at END,
    locked_by = NULL, locked_at = NULL, lock_expires_at = NULL,
    last_error = input.error, last_error_code = input.error_code, updated_at = NOW()
FROM jsonb_to_recordset($1::jsonb) AS input(
    id BIGINT, attempt_count INT, next_attempt_at TIMESTAMPTZ,
    error TEXT, error_code TEXT, target_status TEXT, mark_dlq BOOLEAN)
WHERE d.id = input.id
  AND input.attempt_count = d.attempt_count + 1
  AND d.status IN ('leased', 'sending')
  AND d.locked_by = $2
RETURNING d.id
