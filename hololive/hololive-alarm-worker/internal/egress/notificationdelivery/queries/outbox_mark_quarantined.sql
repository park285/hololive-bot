UPDATE notification_delivery_outbox
SET status = 'QUARANTINED', error = $3,
    locked_at = NULL, locked_by = NULL, lock_expires_at = NULL,
    sending_started_at = NULL
WHERE id = $1 AND locked_by = $2 AND status = 'SENDING'
