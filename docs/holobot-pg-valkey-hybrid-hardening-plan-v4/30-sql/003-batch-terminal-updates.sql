-- Batch retry update.
WITH input AS (
    SELECT *
    FROM unnest(
        $1::bigint[],      -- id
        $2::int[],         -- attempt_count
        $3::timestamptz[], -- next_attempt_at
        $4::text[]         -- error
    ) AS t(id, attempt_count, next_attempt_at, error)
)
UPDATE alarm_dispatch_deliveries d
SET status = 'retry',
    attempt_count = input.attempt_count,
    next_attempt_at = input.next_attempt_at,
    locked_by = NULL,
    locked_at = NULL,
    lock_expires_at = NULL,
    last_error = input.error,
    updated_at = NOW()
FROM input
WHERE d.id = input.id
  AND d.status = 'leased'
  AND d.locked_by = $5
  AND d.lock_expires_at > NOW();

-- Batch DLQ update.
WITH input AS (
    SELECT *
    FROM unnest($1::bigint[], $2::text[]) AS t(id, error)
)
UPDATE alarm_dispatch_deliveries d
SET status = 'dlq',
    dlq_at = NOW(),
    locked_by = NULL,
    locked_at = NULL,
    lock_expires_at = NULL,
    last_error = input.error,
    updated_at = NOW()
FROM input
WHERE d.id = input.id
  AND d.status = 'leased'
  AND d.locked_by = $3;

-- Batch quarantine update.
WITH input AS (
    SELECT *
    FROM unnest($1::bigint[], $2::text[]) AS t(id, error)
)
UPDATE alarm_dispatch_deliveries d
SET status = 'quarantined',
    quarantined_at = NOW(),
    locked_by = NULL,
    locked_at = NULL,
    lock_expires_at = NULL,
    last_error = input.error,
    updated_at = NOW()
FROM input
WHERE d.id = input.id
  AND d.status = 'sending'
  AND d.locked_by = $3;
