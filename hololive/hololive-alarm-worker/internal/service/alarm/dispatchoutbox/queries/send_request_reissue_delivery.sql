UPDATE alarm_dispatch_deliveries SET status = 'retry', attempt_count = $3, next_attempt_at = $4,
 locked_by = NULL, locked_at = NULL, lock_expires_at = NULL, last_error = $5, last_error_code = $6, updated_at = NOW()
WHERE id = $1 AND status = 'sending' AND locked_by = $2 AND attempt_count + 1 = $3
