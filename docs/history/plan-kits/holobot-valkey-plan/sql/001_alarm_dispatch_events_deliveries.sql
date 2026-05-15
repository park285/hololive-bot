-- Alarm dispatch V3 ledger schema.
-- This SQL is a template. Copy it into the repository's migration system with the correct filename/order.
-- Final architecture:
--   alarm_dispatch_events     = room-agnostic logical event payload, stored once
--   alarm_dispatch_deliveries = per-room delivery state ledger

BEGIN;

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

    -- Defense-in-depth only. Domain validation must also reject room-specific payload.
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

    event_id BIGINT NOT NULL
        REFERENCES alarm_dispatch_events(id)
        ON DELETE RESTRICT,

    room_id VARCHAR(64) NOT NULL,
    dedupe_key TEXT NOT NULL,

    claim_keys TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],

    -- Keep delivery_context small and room-specific only when truly needed.
    -- Full event payload must remain in alarm_dispatch_events.payload.
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

    CONSTRAINT alarm_dispatch_deliveries_attempt_check
        CHECK (attempt_count >= 0),

    CONSTRAINT alarm_dispatch_deliveries_room_id_check
        CHECK (length(room_id) > 0 AND length(room_id) <= 64),

    CONSTRAINT alarm_dispatch_deliveries_dedupe_key_check
        CHECK (length(dedupe_key) > 0 AND length(dedupe_key) <= 768),

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

COMMIT;

-- Rollback template:
-- BEGIN;
-- DROP TABLE IF EXISTS alarm_dispatch_deliveries;
-- DROP TABLE IF EXISTS alarm_dispatch_events;
-- COMMIT;
