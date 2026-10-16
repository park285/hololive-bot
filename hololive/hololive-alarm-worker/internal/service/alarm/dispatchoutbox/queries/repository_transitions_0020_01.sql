WITH locked AS MATERIALIZED (
    SELECT id, lock_expires_at
    FROM alarm_dispatch_deliveries
    WHERE id = ANY($1) AND status = 'leased' AND locked_by = $3
    ORDER BY id
    FOR UPDATE
), clock AS MATERIALIZED (
    -- prepared 경로는 unit·전체 member fence의 시각을 재사용한다.
    -- 독립 MarkSending은 전체 대상 잠금 뒤 채취하며 유효한 일부 행의 전이를 유지한다.
    SELECT COALESCE($4::timestamptz, clock_timestamp()) AS transitioned_at
    FROM (SELECT count(id) AS locked_count FROM locked) AS acquired
    WHERE acquired.locked_count > 0
)
UPDATE alarm_dispatch_deliveries d
SET status = 'sending',
    sending_started_at = clock.transitioned_at,
    lock_expires_at = clock.transitioned_at + ($2::INT * INTERVAL '1 second'),
    updated_at = clock.transitioned_at
FROM locked CROSS JOIN clock
WHERE d.id = locked.id
  AND locked.lock_expires_at > clock.transitioned_at