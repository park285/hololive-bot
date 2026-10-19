-- 잠근 행에서 fence 조건을 하나씩 계산해 모두 참일 때만 요청을 고정하고, 계산한 조건은 미스 원인 분류용으로 돌려준다.
-- 행이 없으면 결과 행도 없다. 요청 저장은 시도 횟수를 fence로 쓰지 않는다.
WITH locked AS MATERIALIZED (
    SELECT id, status, locked_by, lock_expires_at, payload->'request' AS request
    FROM notification_delivery_outbox
    WHERE id = $1
    FOR UPDATE
), checked AS MATERIALIZED (
    SELECT id, status,
           status IN ('PENDING', 'SENDING') AS status_matches,
           locked_by IS NOT DISTINCT FROM $2 AS owner_matches,
           COALESCE(status = 'SENDING' OR lock_expires_at > clock_timestamp(), FALSE) AS lease_valid,
           TRUE AS attempt_matches,
           COALESCE((request = $3::jsonb)
                    OR ($3::jsonb = 'null'::jsonb AND request IS NULL AND status = 'PENDING'), FALSE) AS request_matches
    FROM locked
), saved AS (
    UPDATE notification_delivery_outbox AS o
    SET payload = jsonb_set(o.payload, '{request}', $4::jsonb)
    FROM checked
    WHERE o.id = checked.id
      AND checked.status_matches AND checked.owner_matches AND checked.lease_valid AND checked.request_matches
)
SELECT status, status_matches, owner_matches, lease_valid, attempt_matches, request_matches
FROM checked
