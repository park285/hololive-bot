WITH locked AS MATERIALIZED (
    SELECT id, status, locked_by, attempt_count, payload
    FROM notification_delivery_outbox WHERE id = $1 FOR UPDATE
), eligible AS MATERIALIZED (
    SELECT id, clock_timestamp() AS transitioned_at
    FROM locked
    WHERE locked_by = $2 AND status = 'SENDING'
      AND payload->'request' = $3::jsonb AND attempt_count = $9
)
UPDATE notification_delivery_outbox AS o
SET payload = jsonb_set(o.payload, '{request}', $4::jsonb),
    attempt_count = o.attempt_count + 1,
    status = $5,
    next_attempt_at = CASE WHEN $8 THEN eligible.transitioned_at + ($6::double precision * INTERVAL '1 millisecond')
                          ELSE o.next_attempt_at END,
    error = $7,
    locked_at = NULL, locked_by = NULL, lock_expires_at = NULL, sending_started_at = NULL
FROM eligible
WHERE o.id = eligible.id
