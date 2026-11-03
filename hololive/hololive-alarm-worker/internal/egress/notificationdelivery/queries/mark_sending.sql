-- 발송 전 claim을 SENDING으로 옮긴다. 상태·소유자·claim 당시 시도 횟수·lease를 sendfence 규약으로 확인하고, lease
-- 판정과 새 sending lease는 잠금 뒤 채취한 clock 하나에서 계산한다. $4는 sending lease 밀리초다.
WITH locked AS MATERIALIZED (
    SELECT id, status, locked_by, attempt_count, lock_expires_at
    FROM notification_delivery_outbox
    WHERE id = $1
    FOR UPDATE
), clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
    FROM (SELECT count(id) AS locked_count FROM locked) AS acquired
), checked AS MATERIALIZED (
    SELECT locked.id, locked.status, clock.now,
           locked.status = 'PENDING' AS status_matches,
           locked.locked_by IS NOT DISTINCT FROM $2 AS owner_matches,
           locked.attempt_count = $3 AS attempt_matches,
           COALESCE(locked.lock_expires_at > clock.now, FALSE) AS lease_valid,
           TRUE AS request_matches
    FROM locked CROSS JOIN clock
), updated AS (
    UPDATE notification_delivery_outbox AS o
    SET status = 'SENDING',
        sending_started_at = checked.now,
        lock_expires_at = checked.now + ($4::double precision * INTERVAL '1 millisecond')
    FROM checked
    WHERE o.id = checked.id
      AND checked.status_matches AND checked.owner_matches AND checked.attempt_matches
      AND checked.lease_valid AND checked.request_matches
    RETURNING o.id
)
SELECT checked.id, checked.status, checked.status_matches, checked.owner_matches, checked.attempt_matches,
       checked.lease_valid, TRUE AS membership_matches, TRUE AS members_non_terminal, checked.request_matches,
       checked.id IN (SELECT id FROM updated) AS applied
FROM checked
