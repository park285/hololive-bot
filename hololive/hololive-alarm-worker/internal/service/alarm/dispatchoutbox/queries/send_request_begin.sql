-- 고정된 send unit 요청으로 발송 상태에 들어가는 단일 문장이다. 다른 경로와 같은 단위→멤버 순서로 잠근다.
-- 단위 id를 initplan으로 읽어 멤버 잠금 전에 단위 잠금을 끝낸다. 멤버 전원 잠금 뒤 채취한 clock 하나로 lease를
-- 판정하고 sending 진입 시각과 새 lease($5 밀리초)를 기록한다. 단위 멤버 전원의 조건이 참일 때만 전원을 바꾼다.
-- 요청 조건은 고정 여부와 호출자가 claim에서 본 client_request_id 등가다.
WITH unit AS MATERIALIZED (
    SELECT u.id, u.client_request_id, u.request_body_hash, u.request_delivery_ids
    FROM alarm_dispatch_send_units u
    WHERE u.id = $1
    FOR UPDATE
), input AS MATERIALIZED (
    SELECT input.id, input.attempt_count
    FROM jsonb_to_recordset($2::jsonb) AS input(id BIGINT, attempt_count INT)
), locked AS MATERIALIZED (
    SELECT d.id, d.status, d.locked_by, d.attempt_count, d.lock_expires_at
    FROM alarm_dispatch_deliveries d
    WHERE d.send_unit_id = (SELECT unit.id FROM unit)
    ORDER BY d.id
    FOR UPDATE
), clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
    FROM (SELECT count(locked.id) AS locked_count FROM locked) AS acquired
), unit_checked AS MATERIALIZED (
    SELECT members.ids = claimed.ids
               AND (unit.request_delivery_ids IS NULL
                    OR (unit.request_delivery_ids @> claimed.ids AND unit.request_delivery_ids <@ claimed.ids))
               AS membership_matches,
           NOT EXISTS (
               SELECT 1 FROM locked
               WHERE locked.status IN ('sent', 'dlq', 'quarantined', 'cancelled')
           ) AND (unit.request_delivery_ids IS NULL OR unit.request_delivery_ids <@ members.ids) AS members_non_terminal,
           unit.request_body_hash IS NOT NULL AND unit.client_request_id = $4 AS request_matches
    FROM unit
    CROSS JOIN (SELECT COALESCE(array_agg(locked.id ORDER BY locked.id), '{}') AS ids FROM locked) AS members
    CROSS JOIN (SELECT COALESCE(array_agg(input.id ORDER BY input.id), '{}') AS ids FROM input) AS claimed
), checked AS MATERIALIZED (
    SELECT locked.id, locked.status,
           locked.status = 'leased' AS status_matches,
           COALESCE(locked.locked_by = $3, FALSE) AS owner_matches,
           COALESCE(locked.attempt_count = input.attempt_count, FALSE) AS version_matches,
           COALESCE(locked.lock_expires_at > clock.now, FALSE) AS lease_valid,
           COALESCE(unit_checked.membership_matches, FALSE) AS membership_matches,
           COALESCE(unit_checked.members_non_terminal, FALSE) AS members_non_terminal,
           COALESCE(unit_checked.request_matches, FALSE) AS request_matches
    FROM locked
    LEFT JOIN input ON input.id = locked.id
    CROSS JOIN clock
    LEFT JOIN unit_checked ON TRUE
), updated AS (
    UPDATE alarm_dispatch_deliveries d
    SET status = 'sending',
        sending_started_at = clock.now,
        lock_expires_at = clock.now + ($5::BIGINT * INTERVAL '1 millisecond'),
        updated_at = clock.now
    FROM checked CROSS JOIN clock
    WHERE d.id = checked.id
      AND NOT EXISTS (
          SELECT 1 FROM checked AS other
          WHERE NOT (other.status_matches AND other.owner_matches AND other.version_matches AND other.lease_valid
                     AND other.membership_matches AND other.members_non_terminal AND other.request_matches)
      )
    RETURNING d.id
)
SELECT checked.id, checked.status, checked.status_matches, checked.owner_matches, checked.version_matches,
       checked.lease_valid, checked.membership_matches, checked.members_non_terminal, checked.request_matches,
       updated.id IS NOT NULL AS applied
FROM checked
LEFT JOIN updated ON updated.id = checked.id
ORDER BY checked.id
