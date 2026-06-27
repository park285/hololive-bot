CREATE TABLE IF NOT EXISTS alarm_dispatch_events (
    id BIGSERIAL PRIMARY KEY,
    event_key TEXT NOT NULL,
    payload_hash CHAR(64) NOT NULL,
    alarm_type alarm_type NOT NULL,
    channel_id VARCHAR(64) NOT NULL DEFAULT '',
    stream_id VARCHAR(64) NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT '',
    payload_schema_version SMALLINT NOT NULL DEFAULT 1,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT alarm_dispatch_events_event_key_check
        CHECK (length(event_key) > 0 AND length(event_key) <= 512),
    CONSTRAINT alarm_dispatch_events_payload_hash_check
        CHECK (payload_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT alarm_dispatch_events_payload_room_agnostic_check
        CHECK (
            NOT (payload ? 'room_id')
            AND NOT (payload ? 'roomId')
            AND NOT (payload ? 'room')
        ),
    UNIQUE (event_key)
);

CREATE TABLE IF NOT EXISTS alarm_dispatch_deliveries (
    id BIGSERIAL PRIMARY KEY,
    event_id BIGINT NOT NULL REFERENCES alarm_dispatch_events(id) ON DELETE RESTRICT,
    room_id VARCHAR(64) NOT NULL,
    dedupe_key TEXT NOT NULL,
    claim_keys TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    delivery_context JSONB NOT NULL DEFAULT '{}'::JSONB,
    status TEXT NOT NULL DEFAULT 'pending',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_by TEXT,
    locked_at TIMESTAMPTZ,
    lock_expires_at TIMESTAMPTZ,
    sending_started_at TIMESTAMPTZ,
    sent_at TIMESTAMPTZ,
    dlq_at TIMESTAMPTZ,
    quarantined_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    last_error_code TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT alarm_dispatch_deliveries_status_check CHECK (
        status IN (
            'shadowed',
            'pending',
            'retry',
            'leased',
            'sending',
            'sent',
            'dlq',
            'quarantined',
            'cancelled'
        )
    ),
    CONSTRAINT alarm_dispatch_deliveries_attempt_check CHECK (attempt_count >= 0),
    CONSTRAINT alarm_dispatch_deliveries_room_id_check CHECK (length(room_id) > 0 AND length(room_id) <= 64),
    CONSTRAINT alarm_dispatch_deliveries_dedupe_key_check CHECK (length(dedupe_key) > 0 AND length(dedupe_key) <= 768),
    UNIQUE (dedupe_key)
);

CREATE INDEX IF NOT EXISTS idx_alarm_dispatch_deliveries_due
    ON alarm_dispatch_deliveries (next_attempt_at ASC, id ASC)
    WHERE status IN ('pending', 'retry');

CREATE INDEX IF NOT EXISTS idx_alarm_dispatch_deliveries_leased_expired
    ON alarm_dispatch_deliveries (lock_expires_at ASC, id ASC)
    WHERE status = 'leased';

CREATE INDEX IF NOT EXISTS idx_alarm_dispatch_deliveries_sending_stale
    ON alarm_dispatch_deliveries (sending_started_at ASC, id ASC)
    WHERE status = 'sending';

CREATE INDEX IF NOT EXISTS idx_alarm_dispatch_deliveries_event_id
    ON alarm_dispatch_deliveries (event_id);

CREATE INDEX IF NOT EXISTS idx_alarm_dispatch_deliveries_room_created
    ON alarm_dispatch_deliveries (room_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_alarm_dispatch_deliveries_status_created
    ON alarm_dispatch_deliveries (status, created_at DESC);

CREATE TABLE IF NOT EXISTS alarm_dispatch_admin_actions (
    id BIGSERIAL PRIMARY KEY,
    delivery_id BIGINT REFERENCES alarm_dispatch_deliveries(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    operator_id TEXT NOT NULL,
    reason TEXT NOT NULL,
    from_status TEXT NOT NULL DEFAULT '',
    to_status TEXT NOT NULL DEFAULT '',
    duplicate_risk_ack BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT alarm_dispatch_admin_actions_action_check CHECK (length(action) > 0 AND length(action) <= 128),
    CONSTRAINT alarm_dispatch_admin_actions_operator_check CHECK (length(operator_id) > 0 AND length(operator_id) <= 128),
    CONSTRAINT alarm_dispatch_admin_actions_reason_check CHECK (length(reason) > 0 AND length(reason) <= 1024)
);

CREATE INDEX IF NOT EXISTS idx_alarm_dispatch_admin_actions_delivery_created
    ON alarm_dispatch_admin_actions (delivery_id, created_at DESC);

COMMENT ON TABLE alarm_dispatch_events IS 'Alarm dispatch room-agnostic event ledger.';
COMMENT ON TABLE alarm_dispatch_deliveries IS 'Alarm dispatch per-room delivery state ledger.';
COMMENT ON TABLE alarm_dispatch_admin_actions IS 'Operator audit log for manual alarm dispatch delivery actions.';
