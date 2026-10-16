WITH locked AS MATERIALIZED (
    SELECT id, status, locked_by, lock_expires_at
    FROM alarm_dispatch_deliveries
    WHERE send_unit_id = $1
    ORDER BY id
    FOR UPDATE
), clock AS MATERIALIZED (
    -- 전체 member 잠금이 끝난 뒤 한 번만 lease 판정 시각을 채취한다.
    SELECT clock_timestamp() AS checked_at
    FROM (SELECT count(id) AS locked_count FROM locked) AS acquired
    WHERE acquired.locked_count > 0
)
SELECT locked.id, locked.status, COALESCE(locked.locked_by, ''),
       COALESCE(locked.lock_expires_at > clock.checked_at, false), clock.checked_at
FROM locked CROSS JOIN clock
ORDER BY locked.id
