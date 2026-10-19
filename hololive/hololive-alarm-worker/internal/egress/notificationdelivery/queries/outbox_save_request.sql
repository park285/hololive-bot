-- 발송 전 claim에 최초 요청을 고정한다. 요청은 고정 뒤 바뀌지 않으므로 요청 조건은 아직 고정되지 않았음뿐이다.
-- $4는 고정할 요청 JSON이다.
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
           locked.status = 'PENDING' AS status_matches,
           locked.locked_by IS NOT DISTINCT FROM $2 AS owner_matches,
           locked.attempt_count = $3 AS attempt_matches,
           COALESCE(locked.lock_expires_at > clock.now, FALSE) AS lease_valid,
           locked.request IS NULL AS request_matches
    FROM locked CROSS JOIN clock
), updated AS (
    UPDATE notification_delivery_outbox AS o
    SET payload = jsonb_set(o.payload, '{request}', $4::jsonb)
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
