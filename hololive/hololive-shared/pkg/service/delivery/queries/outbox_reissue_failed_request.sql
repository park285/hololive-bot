UPDATE notification_delivery_outbox
SET payload = jsonb_set(payload, '{request}', $4::jsonb),
    attempt_count = attempt_count + 1,
    status = CASE WHEN $8 OR attempt_count + 1 >= $5 THEN 'FAILED' ELSE 'PENDING' END,
    next_attempt_at = CASE WHEN $8 OR attempt_count + 1 >= $5 THEN next_attempt_at
                          ELSE clock_timestamp() + ($6::double precision * INTERVAL '1 millisecond') END,
    error = CASE WHEN $8 THEN 'client request ID generations exhausted: ' || $7 ELSE $7 END,
    locked_at = NULL, locked_by = NULL, lock_expires_at = NULL, sending_started_at = NULL
WHERE id = $1 AND locked_by = $2 AND status = 'SENDING'
  AND payload->'request' = $3::jsonb
