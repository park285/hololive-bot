-- 외부 발송 성공 정산이다. lease를 넘긴 그룹 발송도 sent로 확정해야 하므로 lease 만료는 보지 않는다.
-- QuarantineStaleSending이 sending을 회수할 때 locked_by를 비우므로 status·소유자·attempt 일치가 소유권 증명이다.
-- 다른 단위 전이와 같은 단위→멤버 순서로 잠근다. 단위($3) 잠금은 멤버 행 투영의 initplan이라 정렬 뒤 멤버 잠금보다
-- 먼저 끝난다. 행은 단위 소속까지 각자의 fence가 맞으면 sent가 되고, 입력 전원이 sent가 된 때만 같은 clock으로
-- 단위에 Iris requestId($4)와 첫 handoff 확인 시각(clock + $5 밀리초)을 기록한다. 일부만 적용되면 단위는 그대로다.
WITH unit AS MATERIALIZED (
    SELECT u.id
    FROM alarm_dispatch_send_units u
    WHERE u.id = $3
    FOR UPDATE
), input AS MATERIALIZED (
    SELECT input.id, input.attempt_count
    FROM jsonb_to_recordset($1::jsonb) AS input(id BIGINT, attempt_count INT)
), locked AS MATERIALIZED (
    SELECT d.id, d.status, d.locked_by, d.attempt_count, input.attempt_count AS claimed_attempt_count,
           COALESCE(d.send_unit_id = (SELECT unit.id FROM unit), FALSE) AS unit_matches
    FROM alarm_dispatch_deliveries d
    JOIN input ON input.id = d.id
    ORDER BY d.id
    FOR UPDATE OF d
), clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
    FROM (SELECT count(locked.id) AS locked_count FROM locked) AS acquired
), checked AS MATERIALIZED (
    SELECT locked.id, locked.status,
           locked.status = 'sending' AS status_matches,
           COALESCE(locked.locked_by = $2, FALSE) AS owner_matches,
           locked.attempt_count = locked.claimed_attempt_count AS version_matches,
           locked.unit_matches AS membership_matches
    FROM locked
), updated AS (
    UPDATE alarm_dispatch_deliveries d
    SET status = 'sent',
        sent_at = clock.now,
        locked_by = NULL,
        locked_at = NULL,
        lock_expires_at = NULL,
        updated_at = clock.now
    FROM checked CROSS JOIN clock
    WHERE d.id = checked.id
      AND checked.status_matches AND checked.owner_matches AND checked.version_matches AND checked.membership_matches
    RETURNING d.id
), unit_updated AS (
    UPDATE alarm_dispatch_send_units u
    SET handoff_request_id = $4,
        handoff_next_check_at = clock.now + ($5::BIGINT * INTERVAL '1 millisecond'),
        handoff_checked_at = NULL
    FROM clock
    WHERE u.id = (SELECT unit.id FROM unit)
      AND (SELECT count(updated.id) FROM updated) = (SELECT count(input.id) FROM input)
)
SELECT checked.id, checked.status, checked.status_matches, checked.owner_matches, checked.version_matches,
       TRUE AS lease_valid, checked.membership_matches, TRUE AS members_non_terminal, TRUE AS request_matches,
       updated.id IS NOT NULL AS applied
FROM checked
LEFT JOIN updated ON updated.id = checked.id
ORDER BY checked.id
