UPDATE notification_delivery_outbox
SET payload = jsonb_set(payload, '{request}', $4::jsonb)
WHERE id = $1 AND locked_by = $2
  AND (status = 'SENDING' OR (status = 'PENDING' AND lock_expires_at > clock_timestamp()))
  AND ((payload->'request' = $3::jsonb)
       OR ($3::jsonb = 'null'::jsonb AND payload->'request' IS NULL AND (attempt_count = 0 OR payload->>'known_unsent' = 'true') AND status = 'PENDING'))
