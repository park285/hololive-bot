-- 실패 원장의 잠금과 리비전 검증은 호출자의 직렬화 트랜잭션에서 수행합니다.
-- 발송 식별자, 실패 기록과 본문은 보존하고 외부 발송을 예약하지 않습니다.
WITH target AS (
    SELECT id, status FROM alarm_dispatch_deliveries
    WHERE id = ANY($1::bigint[]) AND status IN ('dlq', 'quarantined')
), updated AS (
    UPDATE alarm_dispatch_deliveries d
    SET status = CASE WHEN $4 = 'cancel' THEN 'cancelled' ELSE 'quarantined' END,
        cancelled_at = CASE WHEN $4 = 'cancel' THEN clock_timestamp() ELSE d.cancelled_at END,
        quarantined_at = CASE WHEN $4 = 'quarantine' AND target.status <> 'quarantined'
                              THEN clock_timestamp() ELSE d.quarantined_at END,
        locked_by = NULL, locked_at = NULL, lock_expires_at = NULL,
        updated_at = GREATEST(clock_timestamp(), d.updated_at + interval '1 microsecond')
    FROM target WHERE d.id = target.id
    RETURNING d.id, target.status AS from_status, d.status AS to_status
), audit AS (
    INSERT INTO alarm_dispatch_admin_actions
        (delivery_id, action, operator_id, reason, from_status, to_status, duplicate_risk_ack)
    SELECT id, 'manual_' || $4, $2, $3, from_status, to_status, FALSE FROM updated
    RETURNING delivery_id
)
SELECT delivery_id::text FROM audit ORDER BY audit.delivery_id
