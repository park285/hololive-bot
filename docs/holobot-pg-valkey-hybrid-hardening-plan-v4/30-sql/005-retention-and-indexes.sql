-- Retention indexes.
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

-- Bounded terminal delete example.
WITH picked AS (
    SELECT id
    FROM alarm_dispatch_deliveries
    WHERE status = 'sent'
      AND sent_at < NOW() - INTERVAL '90 days'
    ORDER BY sent_at ASC, id ASC
    LIMIT $1
)
DELETE FROM alarm_dispatch_deliveries d
USING picked
WHERE d.id = picked.id;

-- Bounded orphan event cleanup.
WITH picked AS (
    SELECT e.id
    FROM alarm_dispatch_events e
    WHERE e.created_at < NOW() - INTERVAL '90 days'
      AND NOT EXISTS (
          SELECT 1
          FROM alarm_dispatch_deliveries d
          WHERE d.event_id = e.id
      )
    ORDER BY e.created_at ASC, e.id ASC
    LIMIT $1
)
DELETE FROM alarm_dispatch_events e
USING picked
WHERE e.id = picked.id;
