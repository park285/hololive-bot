CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_alarm_dispatch_deliveries_send_unit_due
    ON alarm_dispatch_deliveries (send_unit_id, next_attempt_at, id)
    WHERE send_unit_id IS NOT NULL AND status IN ('pending', 'retry');
