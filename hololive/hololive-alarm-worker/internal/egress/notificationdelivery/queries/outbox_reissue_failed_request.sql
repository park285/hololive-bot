-- 잠근 행에서 fence 조건을 하나씩 계산해 모두 참일 때만 새 세대와 다음 상태를 저장하고, 계산한 조건은 미스 원인
-- 분류용으로 돌려준다. 행이 없으면 결과 행도 없다. SENDING 정산은 lease 만료를 fence로 쓰지 않는다.
WITH locked AS MATERIALIZED (
    SELECT id, status, locked_by, attempt_count, payload->'request' AS request
    FROM notification_delivery_outbox
    WHERE id = $1
    FOR UPDATE
), checked AS MATERIALIZED (
    SELECT id, status, clock_timestamp() AS transitioned_at,
           status = 'SENDING' AS status_matches,
           locked_by IS NOT DISTINCT FROM $2 AS owner_matches,
           TRUE AS lease_valid,
           attempt_count = $9 AS attempt_matches,
           request IS NOT DISTINCT FROM $3::jsonb AS request_matches
    FROM locked
), reissued AS (
    UPDATE notification_delivery_outbox AS o
    SET payload = jsonb_set(o.payload, '{request}', $4::jsonb),
        attempt_count = o.attempt_count + 1,
        status = $5,
        next_attempt_at = CASE WHEN $8 THEN checked.transitioned_at + ($6::double precision * INTERVAL '1 millisecond')
                              ELSE o.next_attempt_at END,
        error = $7,
        locked_at = NULL, locked_by = NULL, lock_expires_at = NULL, sending_started_at = NULL
    FROM checked
    WHERE o.id = checked.id
      AND checked.status_matches AND checked.owner_matches AND checked.attempt_matches AND checked.request_matches
)
SELECT status, status_matches, owner_matches, lease_valid, attempt_matches, request_matches
FROM checked
