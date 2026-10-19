-- 요청 읽기·고정(Load·Pin) 전 send unit fence다. 호출자는 같은 트랜잭션에서 send_request_lock.sql로 단위를 먼저 잠근다.
-- 단위 멤버 전원을 id 순서로 잠근 뒤 clock 하나로 lease를 판정하고, 행·단위 조건을 sendfence 관측 열로 돌려준다.
-- 갱신하지 않는 읽기 fence라 단위 멤버 전원의 조건이 참일 때만 applied다.
-- membership은 현재 멤버 집합 = 호출 claim 집합 = 고정 멤버 집합(고정 뒤)이다. 미고정 단위까지 포함해 현재 멤버가
-- 종료됐거나 고정 멤버가 보존 정리로 사라졌으면 단위를 다시 모을 수 없으므로 members_non_terminal이 거짓이다.
WITH input AS MATERIALIZED (
    SELECT input.id, input.attempt_count
    FROM jsonb_to_recordset($2::jsonb) AS input(id BIGINT, attempt_count INT)
), locked AS MATERIALIZED (
    SELECT d.id, d.status, d.locked_by, d.attempt_count, d.lock_expires_at
    FROM alarm_dispatch_deliveries d
    WHERE d.send_unit_id = $1
    ORDER BY d.id
    FOR UPDATE
), clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
    FROM (SELECT count(locked.id) AS locked_count FROM locked) AS acquired
), unit_checked AS MATERIALIZED (
    SELECT members.ids = claimed.ids
               AND (u.request_delivery_ids IS NULL
                    OR (u.request_delivery_ids @> claimed.ids AND u.request_delivery_ids <@ claimed.ids))
               AS membership_matches,
           NOT EXISTS (
               SELECT 1 FROM locked
               WHERE locked.status IN ('sent', 'dlq', 'quarantined', 'cancelled')
           ) AND (u.request_delivery_ids IS NULL OR u.request_delivery_ids <@ members.ids) AS members_non_terminal,
           u.client_request_id = $4 AS request_matches
    FROM alarm_dispatch_send_units u
    CROSS JOIN (SELECT COALESCE(array_agg(locked.id ORDER BY locked.id), '{}') AS ids FROM locked) AS members
    CROSS JOIN (SELECT COALESCE(array_agg(input.id ORDER BY input.id), '{}') AS ids FROM input) AS claimed
    WHERE u.id = $1
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
)
SELECT checked.id, checked.status, checked.status_matches, checked.owner_matches, checked.version_matches,
       checked.lease_valid, checked.membership_matches, checked.members_non_terminal, checked.request_matches,
       NOT EXISTS (
           SELECT 1 FROM checked AS other
           WHERE NOT (other.status_matches AND other.owner_matches AND other.version_matches AND other.lease_valid
                      AND other.membership_matches AND other.members_non_terminal AND other.request_matches)
       ) AS applied
FROM checked
ORDER BY checked.id
