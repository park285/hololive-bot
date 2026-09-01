ALTER TABLE youtube_notification_delivery
    ADD COLUMN IF NOT EXISTS row_version bigint DEFAULT 0;

DO $migration$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'youtube_notification_delivery'::regclass
          AND conname = 'chk_youtube_notification_delivery_row_version'
    ) THEN
        ALTER TABLE youtube_notification_delivery
            ADD CONSTRAINT chk_youtube_notification_delivery_row_version
            CHECK (row_version IS NOT NULL AND row_version >= 0) NOT VALID;
    END IF;
END
$migration$;

ALTER TABLE youtube_notification_delivery
    VALIDATE CONSTRAINT chk_youtube_notification_delivery_row_version;

ALTER TABLE youtube_notification_delivery
    ALTER COLUMN row_version SET NOT NULL;

ALTER TABLE youtube_notification_outbox
    ADD COLUMN IF NOT EXISTS terminal_at timestamptz;

CREATE TABLE IF NOT EXISTS youtube_notification_delivery_ledger (
    kind text NOT NULL,
    logical_id varchar(50) NOT NULL,
    room_id varchar(100) NOT NULL,
    status text NOT NULL,
    first_recorded_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    sent_at timestamptz,
    quarantined_at timestamptz,
    source_delivery_id bigint,
    PRIMARY KEY (kind, logical_id, room_id),
    CONSTRAINT chk_youtube_notification_delivery_ledger_kind_vocab
        CHECK (kind IN (
            'NEW_VIDEO',
            'NEW_SHORT',
            'LIVE_STREAM',
            'COMMUNITY_POST',
            'MILESTONE'
        )),
    CONSTRAINT chk_youtube_notification_delivery_ledger_identity
        CHECK (
            length(btrim(logical_id)) > 0
            AND length(btrim(room_id)) > 0
            AND logical_id = btrim(logical_id)
            AND room_id = btrim(room_id)
        ),
    CONSTRAINT chk_youtube_notification_delivery_ledger_status
        CHECK (status IN ('SENT', 'QUARANTINED')),
    CONSTRAINT chk_youtube_notification_delivery_ledger_shape
        CHECK (
            (
                status = 'SENT'
                AND sent_at IS NOT NULL
            )
            OR
            (
                status = 'QUARANTINED'
                AND sent_at IS NULL
                AND quarantined_at IS NOT NULL
            )
        ),
    CONSTRAINT chk_youtube_notification_delivery_ledger_time_order
        CHECK (
            updated_at >= first_recorded_at
            AND (sent_at IS NULL OR sent_at >= first_recorded_at)
            AND (quarantined_at IS NULL OR quarantined_at >= first_recorded_at)
        ),
    CONSTRAINT chk_youtube_notification_delivery_ledger_source
        CHECK (source_delivery_id IS NULL OR source_delivery_id > 0)
);

CREATE TABLE IF NOT EXISTS youtube_notification_delivery_ledger_state (
    singleton boolean PRIMARY KEY DEFAULT true,
    schema_version integer NOT NULL,
    delivery_high_water_id bigint NOT NULL,
    outbox_high_water_id bigint NOT NULL,
    delivery_cursor_id bigint NOT NULL DEFAULT 0,
    delivery_verify_cursor_id bigint NOT NULL DEFAULT 0,
    outbox_cursor_id bigint NOT NULL DEFAULT 0,
    legacy_coverage_start_at timestamptz,
    coverage_verified_at timestamptz,
    started_at timestamptz NOT NULL,
    completed_at timestamptz,
    updated_at timestamptz NOT NULL,
    CONSTRAINT chk_youtube_notification_delivery_ledger_state_singleton
        CHECK (singleton),
    CONSTRAINT chk_youtube_notification_delivery_ledger_state_version
        CHECK (schema_version > 0),
    CONSTRAINT chk_youtube_notification_delivery_ledger_state_cursors
        CHECK (
            delivery_high_water_id >= 0
            AND outbox_high_water_id >= 0
            AND delivery_cursor_id BETWEEN 0 AND delivery_high_water_id
            AND delivery_verify_cursor_id BETWEEN 0 AND delivery_high_water_id
            AND outbox_cursor_id BETWEEN 0 AND outbox_high_water_id
        ),
    CONSTRAINT chk_youtube_notification_delivery_ledger_state_time_order
        CHECK (
            updated_at >= started_at
            AND (
                (legacy_coverage_start_at IS NULL AND coverage_verified_at IS NULL)
                OR (
                    legacy_coverage_start_at IS NOT NULL
                    AND coverage_verified_at IS NOT NULL
                    AND legacy_coverage_start_at <= coverage_verified_at
                    AND coverage_verified_at <= updated_at
                )
            )
            AND (
                completed_at IS NULL
                OR (
                    completed_at >= coverage_verified_at
                    AND completed_at <= updated_at
                )
            )
        ),
    CONSTRAINT chk_youtube_notification_delivery_ledger_state_completion
        CHECK (
            completed_at IS NULL
            OR (
                delivery_cursor_id = delivery_high_water_id
                AND delivery_verify_cursor_id = delivery_high_water_id
                AND outbox_cursor_id = outbox_high_water_id
                AND legacy_coverage_start_at IS NOT NULL
                AND coverage_verified_at IS NOT NULL
            )
        )
);
