ALTER TABLE alarm_dispatch_events
DROP CONSTRAINT IF EXISTS alarm_dispatch_events_payload_notification_room_agnostic_check;

ALTER TABLE alarm_dispatch_events
ADD CONSTRAINT alarm_dispatch_events_payload_notification_room_agnostic_check
CHECK (
    NOT (payload ? 'room_id')
    AND NOT (payload ? 'roomId')
    AND NOT (payload ? 'room')
    AND NOT (payload ? 'users')
    AND NOT ((payload -> 'notification') ? 'room_id')
    AND NOT ((payload -> 'notification') ? 'roomId')
    AND NOT ((payload -> 'notification') ? 'room')
    AND NOT ((payload -> 'notification') ? 'users')
);

CREATE INDEX IF NOT EXISTS idx_alarm_dispatch_deliveries_sent_retention
    ON alarm_dispatch_deliveries (sent_at, id)
    WHERE status = 'sent';

CREATE INDEX IF NOT EXISTS idx_alarm_dispatch_deliveries_dlq_retention
    ON alarm_dispatch_deliveries (dlq_at, id)
    WHERE status = 'dlq';

CREATE INDEX IF NOT EXISTS idx_alarm_dispatch_deliveries_quarantined_retention
    ON alarm_dispatch_deliveries (quarantined_at, id)
    WHERE status = 'quarantined';

CREATE INDEX IF NOT EXISTS idx_alarm_dispatch_deliveries_cancelled_retention
    ON alarm_dispatch_deliveries (cancelled_at, id)
    WHERE status = 'cancelled';

CREATE INDEX IF NOT EXISTS idx_alarm_dispatch_events_created
    ON alarm_dispatch_events (created_at, id);

COMMENT ON INDEX idx_alarm_dispatch_deliveries_sent_retention IS 'Bounded alarm dispatch sent retention by sent_at/id.';
COMMENT ON INDEX idx_alarm_dispatch_deliveries_dlq_retention IS 'Bounded alarm dispatch DLQ retention by dlq_at/id.';
COMMENT ON INDEX idx_alarm_dispatch_deliveries_quarantined_retention IS 'Bounded alarm dispatch quarantine retention by quarantined_at/id.';
COMMENT ON INDEX idx_alarm_dispatch_deliveries_cancelled_retention IS 'Bounded alarm dispatch cancelled retention by cancelled_at/id.';
COMMENT ON INDEX idx_alarm_dispatch_events_created IS 'Bounded orphan alarm dispatch event cleanup by created_at/id.';
