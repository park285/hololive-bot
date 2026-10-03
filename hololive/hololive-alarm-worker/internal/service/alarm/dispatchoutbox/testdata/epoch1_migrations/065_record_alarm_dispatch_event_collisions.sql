CREATE TABLE IF NOT EXISTS alarm_dispatch_event_collisions (
    id BIGSERIAL PRIMARY KEY,
    existing_event_id BIGINT REFERENCES alarm_dispatch_events(id) ON DELETE SET NULL,
    event_key TEXT NOT NULL,
    existing_payload_hash CHAR(64) NOT NULL,
    incoming_payload_hash CHAR(64) NOT NULL,
    alarm_type alarm_type NOT NULL,
    channel_id VARCHAR(64) NOT NULL DEFAULT '',
    stream_id VARCHAR(64) NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT '',
    payload_schema_version SMALLINT NOT NULL DEFAULT 1,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'detected',
    last_error TEXT NOT NULL DEFAULT 'event_key payload_hash conflict',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT alarm_dispatch_event_collisions_event_key_check
        CHECK (length(event_key) > 0 AND length(event_key) <= 512),
    CONSTRAINT alarm_dispatch_event_collisions_existing_payload_hash_check
        CHECK (existing_payload_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT alarm_dispatch_event_collisions_incoming_payload_hash_check
        CHECK (incoming_payload_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT alarm_dispatch_event_collisions_status_check
        CHECK (status IN ('detected', 'acknowledged', 'resolved')),
    CONSTRAINT alarm_dispatch_event_collisions_payload_room_agnostic_check
        CHECK (
            NOT (payload ? 'room_id')
            AND NOT (payload ? 'roomId')
            AND NOT (payload ? 'room')
            AND NOT (payload ? 'users')
            AND NOT ((payload -> 'notification') ? 'room_id')
            AND NOT ((payload -> 'notification') ? 'roomId')
            AND NOT ((payload -> 'notification') ? 'room')
            AND NOT ((payload -> 'notification') ? 'users')
        ),
    UNIQUE (event_key, incoming_payload_hash)
);

CREATE INDEX IF NOT EXISTS idx_alarm_dispatch_event_collisions_status_created
    ON alarm_dispatch_event_collisions (status, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_alarm_dispatch_event_collisions_existing_event
    ON alarm_dispatch_event_collisions (existing_event_id)
    WHERE existing_event_id IS NOT NULL;

COMMENT ON TABLE alarm_dispatch_event_collisions IS 'Diagnostic records for alarm dispatch event_key payload hash conflicts.';
COMMENT ON INDEX idx_alarm_dispatch_event_collisions_status_created IS 'Supports unresolved alarm dispatch event collision review by status.';
