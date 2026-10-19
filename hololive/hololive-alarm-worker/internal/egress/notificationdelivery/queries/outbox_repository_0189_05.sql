-- 이 worker가 발송한 SENDING claim만 SENT로 확정한다. 발송이 lease를 넘겨도 외부 성공을 잃지 않도록 lease는 보지
-- 않는다. stale sweep 격리나 다른 claim 세대가 앞섰으면 행을 바꾸지 않고 관측만 돌려준다.
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
           locked.status = 'SENDING' AS status_matches,
           locked.locked_by IS NOT DISTINCT FROM $2 AS owner_matches,
           locked.attempt_count = $3 AS attempt_matches,
           TRUE AS lease_valid,
           TRUE AS request_matches
    FROM locked CROSS JOIN clock
), updated AS (
    UPDATE notification_delivery_outbox AS o
    SET status = 'SENT',
        sent_at = checked.now,
        locked_at = NULL,
        locked_by = NULL,
        lock_expires_at = NULL,
        sending_started_at = NULL,
        error = NULL
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
