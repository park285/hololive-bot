-- 118_alarm_dispatch_state_shape_check.sql
-- terminal timestamp의 역방향 조건(예: dlq_at IS NOT NULL이면 status='dlq')은 의도적으로 없다 —
-- manual requeue가 dlq_at/quarantined_at 이력을 보존한 채 retry로 되돌리는 흐름을 깨기 때문.

ALTER TABLE alarm_dispatch_deliveries
    DROP CONSTRAINT IF EXISTS chk_alarm_dispatch_deliveries_state_shape;
ALTER TABLE alarm_dispatch_deliveries
    ADD CONSTRAINT chk_alarm_dispatch_deliveries_state_shape
    CHECK (
        (
            status <> 'leased'
            OR (
                locked_by IS NOT NULL
                AND locked_at IS NOT NULL
                AND lock_expires_at IS NOT NULL
            )
        )
        AND (
            status <> 'sending'
            OR (
                locked_by IS NOT NULL
                AND locked_at IS NOT NULL
                AND lock_expires_at IS NOT NULL
                AND sending_started_at IS NOT NULL
            )
        )
        AND (status <> 'sent' OR sent_at IS NOT NULL)
        AND (status <> 'dlq' OR dlq_at IS NOT NULL)
        AND (status <> 'quarantined' OR quarantined_at IS NOT NULL)
        AND (status <> 'cancelled' OR cancelled_at IS NOT NULL)
    ) NOT VALID;
ALTER TABLE alarm_dispatch_deliveries
    VALIDATE CONSTRAINT chk_alarm_dispatch_deliveries_state_shape;
