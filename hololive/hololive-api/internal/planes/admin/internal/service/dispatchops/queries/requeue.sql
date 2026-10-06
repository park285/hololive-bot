-- 호출자는 serializable 트랜잭션에서 모든 구성원을 ID 순서로 잠근다.
-- 외부 요청 식별자와 시도·오류 이력은 변경하지 않는다.
WITH target AS (
    SELECT id, status
    FROM alarm_dispatch_deliveries
    WHERE id = ANY($1::bigint[]) AND status IN ('dlq', 'quarantined')
), updated AS (
    UPDATE alarm_dispatch_deliveries d
    SET status = 'retry',
        next_attempt_at = clock_timestamp(),
        locked_by = NULL,
        locked_at = NULL,
        lock_expires_at = NULL,
        updated_at = GREATEST(clock_timestamp(), d.updated_at + interval '1 microsecond')
    FROM target
    WHERE d.id = target.id
    RETURNING d.id, target.status AS from_status
), audit AS (
    INSERT INTO alarm_dispatch_admin_actions (
        delivery_id, action, operator_id, reason, from_status, to_status, duplicate_risk_ack
    )
    SELECT id, 'manual_requeue', $2, $3, from_status, 'retry', TRUE
    FROM updated
    RETURNING delivery_id
)
SELECT delivery_id::text FROM audit ORDER BY audit.delivery_id
