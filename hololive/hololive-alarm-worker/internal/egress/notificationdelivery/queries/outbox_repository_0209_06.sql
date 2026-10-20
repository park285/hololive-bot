-- claim 당시 시도 횟수로 정한 실패 정책을 같은 claim에만 적용한다. 발송 전(PENDING) claim은 lease가 살아 있어야 하고,
-- 발송 뒤(SENDING) 확정 실패는 lease와 무관하게 기존 소유자가 정산한다.
-- $4 다음 상태, $5 재시도 지연 밀리초, $6 오류, $7 다음 시도 시각 갱신 여부, $8 저장된 요청 소진 표시 여부.
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
           locked.status IN ('PENDING', 'SENDING') AS status_matches,
           locked.locked_by IS NOT DISTINCT FROM $2 AS owner_matches,
           locked.attempt_count = $3 AS attempt_matches,
           COALESCE(locked.status = 'SENDING' OR locked.lock_expires_at > clock.now, FALSE) AS lease_valid,
           TRUE AS request_matches
    FROM locked CROSS JOIN clock
), updated AS (
    UPDATE notification_delivery_outbox AS o
    SET attempt_count = o.attempt_count + 1,
        error = $6,
        status = $4,
        next_attempt_at = CASE
            WHEN $7 THEN checked.now + ($5::double precision * INTERVAL '1 millisecond')
            ELSE o.next_attempt_at
        END,
        -- 종료 실패에서 저장된 요청을 소진으로 표시해 producer rearm이 같은 요청을 되살리지 않게 한다.
        payload = CASE
            WHEN $8 THEN jsonb_set(o.payload, '{request,exhausted}', 'true'::jsonb)
            ELSE o.payload
        END,
        locked_at = NULL,
        locked_by = NULL,
        lock_expires_at = NULL,
        sending_started_at = NULL
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
