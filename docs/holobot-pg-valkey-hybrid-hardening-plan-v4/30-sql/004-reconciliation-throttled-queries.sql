-- Expired leased recovery. External send has not started, so retry is safe.
WITH picked AS (
    SELECT id
    FROM alarm_dispatch_deliveries
    WHERE status = 'leased'
      AND lock_expires_at < NOW()
    ORDER BY lock_expires_at ASC, id ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
UPDATE alarm_dispatch_deliveries d
SET status = 'retry',
    next_attempt_at = NOW(),
    locked_by = NULL,
    locked_at = NULL,
    lock_expires_at = NULL,
    last_error_code = 'lease_expired_before_send',
    last_error = 'lease expired before external send',
    updated_at = NOW()
FROM picked
WHERE d.id = picked.id;

-- Stale sending quarantine. External send outcome is ambiguous.
WITH picked AS (
    SELECT id
    FROM alarm_dispatch_deliveries
    WHERE status = 'sending'
      AND sending_started_at < NOW() - ($1::INT * INTERVAL '1 second')
    ORDER BY sending_started_at ASC, id ASC
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)
UPDATE alarm_dispatch_deliveries d
SET status = 'quarantined',
    quarantined_at = NOW(),
    locked_by = NULL,
    locked_at = NULL,
    lock_expires_at = NULL,
    last_error_code = 'stale_sending_unknown_outcome',
    last_error = 'stale sending; external send outcome unknown',
    updated_at = NOW()
FROM picked
WHERE d.id = picked.id;
