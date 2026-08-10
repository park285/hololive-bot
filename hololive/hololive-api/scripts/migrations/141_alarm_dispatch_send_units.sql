CREATE TABLE IF NOT EXISTS alarm_dispatch_send_units (
    id BIGSERIAL PRIMARY KEY,
    unit_key CHAR(64) NOT NULL,
    dispatch_group_key TEXT NOT NULL,
    room_id VARCHAR(100) NOT NULL,
    client_request_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT alarm_dispatch_send_units_unit_key_check
        CHECK (unit_key ~ '^[0-9a-f]{64}$'),
    CONSTRAINT alarm_dispatch_send_units_group_key_check
        CHECK (length(dispatch_group_key) > 0 AND length(dispatch_group_key) <= 768),
    CONSTRAINT alarm_dispatch_send_units_room_id_check
        CHECK (length(room_id) > 0 AND length(room_id) <= 100),
    CONSTRAINT alarm_dispatch_send_units_client_request_id_check
        CHECK (client_request_id ~ '^[A-Za-z0-9._:-]{8,160}$'),
    UNIQUE (unit_key),
    UNIQUE (client_request_id)
);

ALTER TABLE alarm_dispatch_deliveries
    ADD COLUMN IF NOT EXISTS dispatch_group_key TEXT,
    ADD COLUMN IF NOT EXISTS send_unit_id BIGINT;

DO $migration$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'alarm_dispatch_deliveries_dispatch_group_key_check'
          AND conrelid = 'alarm_dispatch_deliveries'::regclass
    ) THEN
        ALTER TABLE alarm_dispatch_deliveries
            ADD CONSTRAINT alarm_dispatch_deliveries_dispatch_group_key_check
            CHECK (dispatch_group_key IS NULL OR (length(dispatch_group_key) > 0 AND length(dispatch_group_key) <= 768))
            NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'alarm_dispatch_deliveries_send_unit_pair_check'
          AND conrelid = 'alarm_dispatch_deliveries'::regclass
    ) THEN
        ALTER TABLE alarm_dispatch_deliveries
            ADD CONSTRAINT alarm_dispatch_deliveries_send_unit_pair_check
            CHECK ((dispatch_group_key IS NULL) = (send_unit_id IS NULL))
            NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'alarm_dispatch_deliveries_send_unit_fk'
          AND conrelid = 'alarm_dispatch_deliveries'::regclass
    ) THEN
        ALTER TABLE alarm_dispatch_deliveries
            ADD CONSTRAINT alarm_dispatch_deliveries_send_unit_fk
            FOREIGN KEY (send_unit_id) REFERENCES alarm_dispatch_send_units(id) ON DELETE RESTRICT
            NOT VALID;
    END IF;
END
$migration$;

ALTER TABLE alarm_dispatch_deliveries
    VALIDATE CONSTRAINT alarm_dispatch_deliveries_dispatch_group_key_check;

ALTER TABLE alarm_dispatch_deliveries
    VALIDATE CONSTRAINT alarm_dispatch_deliveries_send_unit_pair_check;

ALTER TABLE alarm_dispatch_deliveries
    VALIDATE CONSTRAINT alarm_dispatch_deliveries_send_unit_fk;

COMMENT ON TABLE alarm_dispatch_send_units IS 'Immutable alarm dispatch send identity shared by all deliveries in one external request.';
COMMENT ON COLUMN alarm_dispatch_deliveries.dispatch_group_key IS 'Canonical grouping boundary assigned before claim; NULL only for pre-migration rows.';
COMMENT ON COLUMN alarm_dispatch_deliveries.send_unit_id IS 'Persisted external-send identity; retries retain the same client_request_id through this unit.';
