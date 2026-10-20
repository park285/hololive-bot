-- 409 pre-handoff 충돌을 받은 SENDING claim의 요청 세대를 올리거나 소진으로 표시하고 다음 상태를 같은 문장에 저장한다.
-- 본문·경로·base ID는 고정 뒤 바뀌지 않으므로 요청 조건은 세대 등가와 미소진이다. 발송 뒤 정산이라 lease는 보지 않는다.
-- $4 기대 세대, $5 소진 표시 여부(거짓이면 세대 + 1), $6 다음 상태, $7 재시도 지연 밀리초, $8 오류, $9 다음 시도 시각 갱신 여부.
WITH locked AS MATERIALIZED (
    SELECT id, status, locked_by, attempt_count, lock_expires_at, payload->'request' AS request
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
           COALESCE(locked.request IS NOT NULL
                    AND (locked.request->>'generation')::int = $4
                    AND COALESCE(locked.request->>'exhausted', 'false') <> 'true', FALSE) AS request_matches
    FROM locked CROSS JOIN clock
), updated AS (
    UPDATE notification_delivery_outbox AS o
    SET payload = CASE
            WHEN $5 THEN jsonb_set(o.payload, '{request,exhausted}', 'true'::jsonb)
            ELSE jsonb_set(o.payload, '{request,generation}', to_jsonb($4::int + 1))
        END,
        attempt_count = o.attempt_count + 1,
        status = $6,
        next_attempt_at = CASE
            WHEN $9 THEN checked.now + ($7::double precision * INTERVAL '1 millisecond')
            ELSE o.next_attempt_at
        END,
        error = $8,
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
