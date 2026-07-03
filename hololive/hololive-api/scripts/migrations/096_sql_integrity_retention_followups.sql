-- 096_sql_integrity_retention_followups.sql
-- SQL review follow-up for schema invariants and append-only table access paths.
-- apply-all.sh runs statements in file-order autocommit, so CONCURRENTLY is valid here.

ALTER TABLE youtube_notification_delivery_telemetry
    DROP CONSTRAINT IF EXISTS youtube_notification_delivery_telemetry_outbox_id_fkey;

COMMENT ON COLUMN youtube_notification_delivery_telemetry.outbox_id IS
    'Snapshot of the source youtube_notification_outbox id. Intentionally not an FK so delivery telemetry survives outbox retention cleanup.';

UPDATE members
SET status = 'graduated'
WHERE is_graduated = true AND status <> 'graduated';

UPDATE members
SET is_graduated = true
WHERE status = 'graduated' AND is_graduated = false;

ALTER TABLE members
    DROP CONSTRAINT IF EXISTS chk_members_graduated_sync;

ALTER TABLE members
    ADD CONSTRAINT chk_members_graduated_sync
    CHECK (is_graduated = (status = 'graduated')) NOT VALID;

ALTER TABLE members
    VALIDATE CONSTRAINT chk_members_graduated_sync;

ALTER TABLE alarms
    ALTER COLUMN room_id TYPE VARCHAR(100);

ALTER TABLE alarm_dispatch_deliveries
    DROP CONSTRAINT IF EXISTS alarm_dispatch_deliveries_room_id_check;

ALTER TABLE alarm_dispatch_deliveries
    ALTER COLUMN room_id TYPE VARCHAR(100);

ALTER TABLE alarm_dispatch_deliveries
    ADD CONSTRAINT alarm_dispatch_deliveries_room_id_check
    CHECK (length(room_id) > 0 AND length(room_id) <= 100) NOT VALID;

ALTER TABLE alarm_dispatch_deliveries
    VALIDATE CONSTRAINT alarm_dispatch_deliveries_room_id_check;

ALTER TABLE youtube_milestones
    ALTER COLUMN channel_id TYPE VARCHAR(64);

ALTER TABLE youtube_milestone_approaching
    ALTER COLUMN channel_id TYPE VARCHAR(64);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_type
        WHERE typname = 'alarm_type'
    ) THEN
        RAISE EXCEPTION 'alarm_type enum is missing';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM unnest(ARRAY['LIVE', 'COMMUNITY', 'SHORTS']) AS required(label)
        WHERE NOT EXISTS (
            SELECT 1
            FROM pg_enum e
            JOIN pg_type t ON t.oid = e.enumtypid
            WHERE t.typname = 'alarm_type' AND e.enumlabel = required.label
        )
    ) THEN
        RAISE EXCEPTION 'alarm_type enum is missing a required label';
    END IF;
END $$;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ndo_pending_due_created_id
    ON notification_delivery_outbox (next_attempt_at, created_at, id)
    WHERE status = 'PENDING';

DROP INDEX CONCURRENTLY IF EXISTS idx_ndo_pending_next;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ydt_logged_event_retention
    ON youtube_notification_delivery_telemetry (event_at, id)
    WHERE logged_at IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ysh_time_brin
    ON youtube_stats_history USING BRIN (time);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ycss_captured_at_brin
    ON youtube_channel_stats_snapshots USING BRIN (captured_at);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ylvs_captured_at_brin
    ON youtube_live_viewer_samples USING BRIN (captured_at);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_yls_ended_cleanup
    ON youtube_live_sessions (ended_at, video_id)
    WHERE status = 'ENDED' AND ended_at IS NOT NULL;
