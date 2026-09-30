UPDATE notification_delivery_outbox
SET payload = jsonb_set(payload, '{known_unsent}', 'true'::jsonb)
WHERE id = $1 AND locked_by = $2 AND status = 'PENDING'
  AND lock_expires_at > clock_timestamp() AND payload->'request' IS NULL
