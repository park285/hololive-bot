CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_alarm_dispatch_deliveries_send_unit
    ON alarm_dispatch_deliveries (send_unit_id)
    WHERE send_unit_id IS NOT NULL;
