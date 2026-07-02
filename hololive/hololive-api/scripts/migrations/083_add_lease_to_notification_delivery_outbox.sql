ALTER TABLE notification_delivery_outbox
    ADD COLUMN IF NOT EXISTS locked_by TEXT,
    ADD COLUMN IF NOT EXISTS lock_expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS sending_started_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_ndo_lease_expired
    ON notification_delivery_outbox (lock_expires_at)
    WHERE status = 'PENDING' AND lock_expires_at IS NOT NULL;
